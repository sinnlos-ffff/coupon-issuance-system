package server

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	coupon "coupon-issuance/gen/coupon/v1"
	"coupon-issuance/internal/database"
	redisclient "coupon-issuance/internal/redis"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	campaignActivationKey = "campaign:activation:"
	campaignCounterKey    = "campaign:counter:"
)

type CouponService struct {
	pool                    *pgxpool.Pool
	redis                   *redis.Client
	codeGen                 *codeGenerator
	context                 context.Context
	cancelBackgroundWorkers context.CancelFunc
}

func (s *CouponService) updateCampaignStatus(
	ctx context.Context,
	campaignID string,
) error {
	// First check if the campaign exists and get its current status
	var currentStatus string
	err := s.pool.QueryRow(ctx,
		`SELECT status FROM campaigns WHERE id = $1`,
		campaignID,
	).Scan(&currentStatus)

	if err != nil {
		return fmt.Errorf("failed to get campaign status: %w", err)
	}

	// Only update if the campaign is in 'scheduled' state
	if currentStatus != "scheduled" {
		return nil // Campaign is already active or finished, no need to update
	}

	// Update the status to active
	_, err = s.pool.Exec(ctx,
		`UPDATE campaigns SET status = 'active' WHERE id = $1 AND status = 'scheduled'`,
		campaignID,
	)
	return err
}

func (s *CouponService) startCampaignStatusWorker(ctx context.Context) {
	// Could be adjusted if desired
	serverCtx := s.context
	interval := time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
			now := time.Now().Unix()

			// Get all campaigns that should be activated (score <= now)
			results, err := s.redis.ZRangeByScore(serverCtx, campaignActivationKey,
				&redis.ZRangeBy{
					Min:    "0",
					Max:    fmt.Sprintf("%d", now),
					Offset: 0,
				}).Result()

			if err != nil {
				log.Printf("Error getting campaigns to activate: %v", err)
				time.Sleep(interval)
				continue
			}

			if len(results) == 0 {
				time.Sleep(interval)
				continue
			}

			for _, campaignID := range results {
				if err := s.updateCampaignStatus(serverCtx, campaignID); err != nil {
					log.Printf("Failed to update campaign status for %s: %v", campaignID, err)
					continue
				}

				// Remove from the activation set
				if err := s.redis.ZRem(
					serverCtx,
					campaignActivationKey,
					campaignID,
				).Err(); err != nil {
					log.Printf(
						"Failed to remove campaign %s from activation set: %v",
						campaignID,
						err,
					)
				}
			}
		}
	}
}

func (s *CouponService) startCouponCodeWriter(ctx context.Context) {
	serverCtx := s.context
	// Flush codes every second
	interval := time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final flush on shutdown
			if err := s.codeGen.writeIssuedCodes(serverCtx, s.pool); err != nil {
				log.Printf("Failed to write issued codes during shutdown: %v", err)
			}
			return
		case <-ticker.C:
			// Check if we have any codes to write
			if s.codeGen.hasPendingCodes() {
				if err := s.codeGen.writeIssuedCodes(serverCtx, s.pool); err != nil {
					log.Printf("Failed to write issued codes: %v", err)
				}
			}
		}
	}
}

func NewCouponService() *CouponService {
	ctx := context.Background()
	backgroundCtx, cancel := context.WithCancel(ctx)

	pool, err := database.NewPool(ctx)
	if err != nil {
		log.Fatalf("Failed to create database pool: %v", err)
	}

	redisCfg := redisclient.NewConfig()
	redisClient, err := redisclient.NewClient(redisCfg)
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}

	codeGen := newCodeGenerator()

	service := &CouponService{
		pool:                    pool,
		redis:                   redisClient,
		codeGen:                 codeGen,
		context:                 ctx,
		cancelBackgroundWorkers: cancel,
	}

	go service.startCampaignStatusWorker(backgroundCtx)
	go service.startCouponCodeWriter(backgroundCtx)

	return service
}

type (
	CreateCampaignReq  = connect.Request[coupon.CreateCampaignRequest]
	CreateCampaignResp = connect.Response[coupon.CreateCampaignResponse]
	GetCampaignReq     = connect.Request[coupon.GetCampaignRequest]
	GetCampaignResp    = connect.Response[coupon.GetCampaignResponse]
	IssueCouponReq     = connect.Request[coupon.IssueCouponRequest]
	IssueCouponResp    = connect.Response[coupon.IssueCouponResponse]
)

func (s *CouponService) CreateCampaign(
	ctx context.Context,
	req *CreateCampaignReq,
) (*CreateCampaignResp, error) {
	// Validation
	if len(strings.TrimSpace(req.Msg.Name)) == 0 {
		return nil, connect.NewError(
			connect.CodeInvalidArgument,
			fmt.Errorf("campaign name cannot be empty"),
		)
	}

	if req.Msg.CouponLimit <= 0 {
		return nil, connect.NewError(
			connect.CodeInvalidArgument,
			fmt.Errorf("coupon limit must be greater than 0"),
		)
	}

	startTime, err := time.Parse(time.RFC3339, req.Msg.StartTime)
	if err != nil {
		return nil, connect.NewError(
			connect.CodeInvalidArgument,
			fmt.Errorf("invalid start_time format: %v", err),
		)
	}

	var campaignID pgtype.UUID
	err = s.pool.QueryRow(ctx,
		`INSERT INTO campaigns (name, start_time, coupon_limit)
		VALUES ($1, $2, $3)
		RETURNING id`,
		req.Msg.Name,
		startTime,
		req.Msg.CouponLimit,
	).Scan(&campaignID)

	if err != nil {
		return nil, connect.NewError(
			connect.CodeInternal,
			fmt.Errorf("failed to create campaign: %v", err),
		)
	}

	err = s.redis.ZAdd(ctx, campaignActivationKey, redis.Z{
		Score:  float64(startTime.Unix()),
		Member: campaignID.String(),
	}).Err()

	if err != nil {
		return nil, connect.NewError(
			connect.CodeInternal,
			fmt.Errorf("failed to schedule campaign activation: %v", err),
		)
	}

	counterKey := fmt.Sprintf("%s%s", campaignCounterKey, campaignID.String())
	err = s.redis.Set(ctx, counterKey, req.Msg.CouponLimit, 0).Err()
	if err != nil {
		return nil, connect.NewError(
			connect.CodeInternal,
			fmt.Errorf("failed to initialize coupon counter: %v", err),
		)
	}

	return connect.NewResponse(&coupon.CreateCampaignResponse{
		CampaignId: campaignID.String(),
	}), nil
}

func (s *CouponService) GetCampaign(
	ctx context.Context,
	req *GetCampaignReq,
) (*GetCampaignResp, error) {
	var (
		name      string
		startTime time.Time
		status    string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT name, start_time, status FROM campaigns WHERE id = $1`,
		req.Msg.CampaignId,
	).Scan(&name, &startTime, &status)

	if err != nil {
		return nil, connect.NewError(
			connect.CodeNotFound,
			fmt.Errorf("campaign not found: %v", err),
		)
	}

	// Get issued coupons
	var issuedCoupons []string
	rows, err := s.pool.Query(ctx,
		`SELECT code FROM coupons WHERE campaign_id = $1 ORDER BY created_at`,
		req.Msg.CampaignId,
	)
	if err != nil {
		return nil, connect.NewError(
			connect.CodeInternal,
			fmt.Errorf("failed to get issued coupons: %v", err),
		)
	}
	defer rows.Close()

	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, connect.NewError(
				connect.CodeInternal,
				fmt.Errorf("failed to scan coupon code: %v", err),
			)
		}
		issuedCoupons = append(issuedCoupons, code)
	}
	if err := rows.Err(); err != nil {
		return nil, connect.NewError(
			connect.CodeInternal,
			fmt.Errorf("error iterating coupon codes: %v", err),
		)
	}

	return connect.NewResponse(&coupon.GetCampaignResponse{
		Name:          name,
		StartTime:     startTime.Format(time.RFC3339),
		Status:        status,
		IssuedCoupons: issuedCoupons,
	}), nil
}

func (s *CouponService) updateCampaignToFinished(ctx context.Context, campaignID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaigns SET status = 'finished' WHERE id = $1`,
		campaignID,
	)
	return err
}

func (s *CouponService) IssueCoupon(
	ctx context.Context,
	req *IssueCouponReq,
) (*IssueCouponResp, error) {
	// Check if campaign exists and is active
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT status FROM campaigns WHERE id = $1`,
		req.Msg.CampaignId,
	).Scan(&status)

	if err != nil {
		return nil, connect.NewError(
			connect.CodeNotFound,
			fmt.Errorf("campaign not found: %v", err),
		)
	}

	if status != "active" {
		return nil, connect.NewError(
			connect.CodeFailedPrecondition,
			fmt.Errorf("campaign is not active (status: %s)", status),
		)
	}

	counterKey := fmt.Sprintf("%s%s", campaignCounterKey, req.Msg.CampaignId)

	// Lua script to atomically check and decrement
	script := `
		local current = redis.call('GET', KEYS[1])
		if not current or tonumber(current) <= 0 then
			return -1
		end
		local new_value = redis.call('DECR', KEYS[1])
		if new_value == 0 then
			return -2
		end
		return new_value
	`

	remaining, err := s.redis.Eval(ctx, script, []string{counterKey}).Int64()
	if err != nil {
		return nil, connect.NewError(
			connect.CodeInternal,
			fmt.Errorf("failed to check coupon availability: %v", err),
		)
	}

	if remaining == -1 {
		return nil, connect.NewError(
			connect.CodeResourceExhausted,
			fmt.Errorf("campaign has reached its coupon limit"),
		)
	}

	if remaining == -2 {
		// Update database status
		if err := s.updateCampaignToFinished(ctx, req.Msg.CampaignId); err != nil {
			return nil, connect.NewError(
				connect.CodeInternal,
				fmt.Errorf("failed to update campaign status to finished: %v", err),
			)
		}
	}

	// Generate a unique coupon code
	code, err := s.codeGen.generateCouponCode(ctx, s.pool, req.Msg.CampaignId)
	if err != nil {
		// Increment back
		s.redis.Incr(ctx, counterKey)
		return nil, connect.NewError(
			connect.CodeInternal,
			fmt.Errorf("failed to generate coupon code: %v", err),
		)
	}

	return connect.NewResponse(&coupon.IssueCouponResponse{
		CouponCode: code,
	}), nil
}

func (s *CouponService) Close() {
	if s.cancelBackgroundWorkers != nil {
		s.cancelBackgroundWorkers()
	}
	if s.pool != nil {
		s.pool.Close()
	}
	if s.redis != nil {
		s.redis.Close()
	}
}
