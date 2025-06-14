package server

import (
	"context"
	"fmt"
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

	// clean up redis
	keys, err := redisClient.Keys(ctx, "campaign:*").Result()
	require.NoError(t, err)
	if len(keys) > 0 {
		_, err = redisClient.Del(ctx, keys...).Result()
		require.NoError(t, err)
	}

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
			ctx := context.Background()
			service := setupTestService(t)
			defer service.Close()

			req := connect.NewRequest(tt.request)
			resp, err := service.CreateCampaign(ctx, req)

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

			// Verify campaign in database
			var count int
			err = service.pool.QueryRow(ctx,
				"SELECT COUNT(*) FROM campaigns WHERE id = $1",
				resp.Msg.CampaignId,
			).Scan(&count)
			assert.NoError(t, err)
			assert.Equal(t, 1, count)

			// Verify campaign activation in Redis
			score, err := service.redis.ZScore(ctx, campaignActivationKey, resp.Msg.CampaignId).Result()
			assert.NoError(t, err)
			expectedTime, err := time.Parse(time.RFC3339, tt.request.StartTime)
			require.NoError(t, err)
			assert.Equal(t, float64(expectedTime.Unix()), score)

			// Verify coupon counter in Redis
			counterKey := fmt.Sprintf("%s%s", campaignCounterKey, resp.Msg.CampaignId)
			val, err := service.redis.Get(ctx, counterKey).Int()
			assert.NoError(t, err)
			assert.Equal(t, int32(val), tt.request.CouponLimit)
		})
	}
}
