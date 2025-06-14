package server

import (
	"context"
	"log"

	coupon "coupon-issuance/gen/coupon/v1"
	"coupon-issuance/internal/database"
	redisclient "coupon-issuance/internal/redis"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type CouponService struct {
	pool  *pgxpool.Pool
	redis *redis.Client
}

// NewCouponService creates a new instance of CouponService
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
	// TODO: Implement campaign creation logic
	return connect.NewResponse(&coupon.CreateCampaignResponse{}), nil
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
