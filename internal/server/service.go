package server

import (
	"context"

	coupon "coupon-issuance/gen/coupon/v1"

	"connectrpc.com/connect"
)

type CouponServer struct{}

func (s *CouponServer) CreateCampaign(ctx context.Context, req *connect.Request[coupon.CreateCampaignRequest]) (*connect.Response[coupon.CreateCampaignResponse], error) {
	// TODO: Implement campaign creation logic
	return connect.NewResponse(&coupon.CreateCampaignResponse{}), nil
}

func (s *CouponServer) GetCampaign(ctx context.Context, req *connect.Request[coupon.GetCampaignRequest]) (*connect.Response[coupon.GetCampaignResponse], error) {
	// TODO: Implement campaign retrieval logic
	return connect.NewResponse(&coupon.GetCampaignResponse{}), nil
}

func (s *CouponServer) IssueCoupon(ctx context.Context, req *connect.Request[coupon.IssueCouponRequest]) (*connect.Response[coupon.IssueCouponResponse], error) {
	// TODO: Implement coupon issuance logic
	return connect.NewResponse(&coupon.IssueCouponResponse{}), nil
}
