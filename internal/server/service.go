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

type CouponService struct {
	pool  *pgxpool.Pool
	redis *redis.Client
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

	return &CouponService{
		pool:  pool,
		redis: redisClient,
	}
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
