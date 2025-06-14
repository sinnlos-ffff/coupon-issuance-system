package main

import (
	"context"
	"log"
	"net/http"

	coupon "coupon-issuance/gen/coupon/v1"
	couponConnect "coupon-issuance/gen/coupon/v1/v1connect"

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

func main() {
	server := &CouponServer{}
	path, handler := couponConnect.NewCouponServiceHandler(server)
	mux := http.NewServeMux()
	mux.Handle(path, handler)

	log.Printf("Server listening at http://localhost:8000")
	if err := http.ListenAndServe("localhost:8000", mux); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
