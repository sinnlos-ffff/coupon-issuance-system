package main

import (
	"context"
	"log"
	"net/http"

	coupon "coupon-issuance/gen/coupon/v1"
	couponConnect "coupon-issuance/gen/coupon/v1/v1connect"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
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
	couponServer := &CouponServer{}
	path, handler := couponConnect.NewCouponServiceHandler(couponServer)
	mux := http.NewServeMux()
	mux.Handle(path, handler)

	// HTTP server with H2C support
	h2s := &http2.Server{}
	httpServer := &http.Server{
		Addr:    ":8000",
		Handler: h2c.NewHandler(mux, h2s),
	}

	log.Printf("Server listening at http://0.0.0.0:8000")
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
