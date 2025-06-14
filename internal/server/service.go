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

type campaignStatusUpdate struct {
	CampaignID string    `json:"campaign_id"`
	StartTime  time.Time `json:"start_time"`
}

type CouponService struct {
	pool  *pgxpool.Pool
	redis *redis.Client
}

func (s *CouponService) updateCampaignStatus(ctx context.Context, campaignID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaigns SET status = 'active' WHERE id = $1`,
		campaignID,
	)
	return err
}

func (s *CouponService) startCampaignStatusWorker(ctx context.Context) {
	// Could be shortened if desired
	interval := time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
			now := time.Now().Unix()

			// Get all campaigns that should be activated (score <= now)
			results, err := s.redis.ZRangeByScore(ctx, campaignActivationKey, &redis.ZRangeBy{
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
				if err := s.updateCampaignStatus(ctx, campaignID); err != nil {
					log.Printf("Failed to update campaign status for %s: %v", campaignID, err)
					continue
				}

				// Remove from the activation set
				if err := s.redis.ZRem(ctx, campaignActivationKey, campaignID).Err(); err != nil {
					log.Printf("Failed to remove campaign %s from activation set: %v", campaignID, err)
				}
			}
		}
	}
}

func NewCouponService() *CouponService {
	ctx := context.Background()

	pool, err := database.NewPool(ctx)
	if err != nil {
		log.Fatalf("Failed to create database pool: %v", err)
	}

	redisCfg := redisclient.NewConfig()
	redisClient, err := redisclient.NewClient(redisCfg)
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}

	service := &CouponService{
		pool:  pool,
		redis: redisClient,
	}

	go service.startCampaignStatusWorker(ctx)

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
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("campaign name cannot be empty"))
	}

	if req.Msg.CouponLimit <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("coupon limit must be greater than 0"))
	}

	startTime, err := time.Parse(time.RFC3339, req.Msg.StartTime)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid start_time format: %v", err))
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
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create campaign: %v", err))
	}

	err = s.redis.ZAdd(ctx, campaignActivationKey, redis.Z{
		Score:  float64(startTime.Unix()),
		Member: campaignID.String(),
	}).Err()

	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to schedule campaign activation: %v", err))
	}

	counterKey := fmt.Sprintf("%s%s", campaignCounterKey, campaignID.String())
	err = s.redis.Set(ctx, counterKey, req.Msg.CouponLimit, 0).Err()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to initialize coupon counter: %v", err))
	}

	return connect.NewResponse(&coupon.CreateCampaignResponse{
		CampaignId: campaignID.String(),
	}), nil
}

func (s *CouponService) GetCampaign(
	ctx context.Context,
	req *GetCampaignReq,
) (*GetCampaignResp, error) {
	// TODO: Implement campaign retrieval logic
	return connect.NewResponse(&coupon.GetCampaignResponse{}), nil
}

func (s *CouponService) IssueCoupon(
	ctx context.Context,
	req *IssueCouponReq,
) (*IssueCouponResp, error) {
	// TODO: Implement coupon issuance logic
	return connect.NewResponse(&coupon.IssueCouponResponse{}), nil
}

func (s *CouponService) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.redis != nil {
		s.redis.Close()
	}
}
