package main

import (
	"log"
	"net/http"

	couponConnect "coupon-issuance/gen/coupon/v1/v1connect"
	"coupon-issuance/internal/server"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func main() {
	couponServer := &server.CouponServer{}
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
