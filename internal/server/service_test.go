package server

import (
	"context"
	"testing"
	"time"

	coupon "coupon-issuance/gen/coupon/v1"
	"coupon-issuance/internal/database"
	"coupon-issuance/internal/redis"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestService(t *testing.T) *CouponService {
	// TODO: separate test database and redis
	ctx := context.Background()

	pool, err := database.NewPool(ctx)
	require.NoError(t, err)

	redisCfg := redis.NewConfig()
	redisClient, err := redis.NewClient(redisCfg)
	require.NoError(t, err)

	service := &CouponService{
		pool:  pool,
		redis: redisClient,
	}

	// clean up database
	_, err = pool.Exec(ctx, "DELETE FROM campaigns")
	require.NoError(t, err)

	return service
}

func TestCreateCampaign(t *testing.T) {
	tests := []struct {
		name        string
		request     *coupon.CreateCampaignRequest
		expectErr   bool
		errCode     connect.Code
		description string
	}{
		{
			name: "successful campaign creation",
			request: &coupon.CreateCampaignRequest{
				Name:        "Summer Sale 2024",
				StartTime:   time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				CouponLimit: 1000,
			},
			expectErr:   false,
			description: "should successfully create a campaign",
		},
		{
			name: "invalid start time format",
			request: &coupon.CreateCampaignRequest{
				Name:        "Invalid Time",
				StartTime:   "invalid-time-format",
				CouponLimit: 1000,
			},
			expectErr:   true,
			errCode:     connect.CodeInvalidArgument,
			description: "should return error for invalid time format",
		},
		{
			name: "empty campaign name",
			request: &coupon.CreateCampaignRequest{
				Name:        "",
				StartTime:   time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				CouponLimit: 1000,
			},
			expectErr:   true,
			errCode:     connect.CodeInvalidArgument,
			description: "should return error for empty campaign name",
		},
		{
			name: "negative coupon limit",
			request: &coupon.CreateCampaignRequest{
				Name:        "Negative Limit",
				StartTime:   time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				CouponLimit: -1,
			},
			expectErr:   true,
			errCode:     connect.CodeInvalidArgument,
			description: "should return error for negative coupon limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := setupTestService(t)
			defer service.Close()

			req := connect.NewRequest(tt.request)
			resp, err := service.CreateCampaign(context.Background(), req)

			if tt.expectErr {
				assert.Error(t, err)
				if err != nil {
					connectErr, ok := err.(*connect.Error)
					assert.True(t, ok, "error should be a connect.Error")
					assert.Equal(t, tt.errCode, connectErr.Code())
				}
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, resp)
			assert.NotEmpty(t, resp.Msg.CampaignId)

			var count int
			err = service.pool.QueryRow(context.Background(),
				"SELECT COUNT(*) FROM campaigns WHERE id = $1",
				resp.Msg.CampaignId,
			).Scan(&count)
			assert.NoError(t, err)
			assert.Equal(t, 1, count)
		})
	}
}
