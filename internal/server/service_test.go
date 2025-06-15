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
	backgroundCtx, cancel := context.WithCancel(ctx)

	pool, err := database.NewPool(ctx)
	require.NoError(t, err)

	redisCfg := redis.NewConfig()
	redisClient, err := redis.NewClient(redisCfg)
	require.NoError(t, err)

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

	// Register cleanup to run after test
	t.Cleanup(func() {
		cleanupTestData(t, service)
	})

	return service
}

func cleanupTestData(t *testing.T, service *CouponService) {
	ctx := context.Background()

	// Clean up database
	_, err := service.pool.Exec(ctx, "DELETE FROM coupons")
	require.NoError(t, err)
	_, err = service.pool.Exec(ctx, "DELETE FROM campaigns")
	require.NoError(t, err)

	// Clean up Redis keys
	iter := service.redis.Scan(ctx, 0, "campaign:*", 0).Iterator()
	for iter.Next(ctx) {
		err := service.redis.Del(ctx, iter.Val()).Err()
		require.NoError(t, err)
	}
	require.NoError(t, iter.Err())

	// Stop background workers and close connections
	service.Close()
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
			score, err := service.redis.ZScore(
				ctx,
				campaignActivationKey,
				resp.Msg.CampaignId,
			).Result()
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

func TestCouponService_IssueCoupon(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()

	// Create a test campaign
	campaignID := "00000000-0000-0000-0000-000000000000"
	_, err := service.pool.Exec(ctx,
		`INSERT INTO campaigns (id, name, start_time, coupon_limit, status)
		VALUES ($1, $2, $3, $4, $5)`,
		campaignID,
		"Test Campaign",
		time.Now(),
		2, // Coupon limit
		"active",
	)
	require.NoError(t, err)

	counterKey := fmt.Sprintf("%s%s", campaignCounterKey, campaignID)
	err = service.redis.Set(ctx, counterKey, 2, 0).Err()
	require.NoError(t, err)

	t.Run("successful coupon issuance", func(t *testing.T) {
		resp, err := service.IssueCoupon(ctx, connect.NewRequest(&coupon.IssueCouponRequest{
			CampaignId: campaignID,
		}))
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Msg.CouponCode)
		assert.Equal(t, 10, len([]rune(resp.Msg.CouponCode))) // Check code length
	})

	t.Run("campaign not found", func(t *testing.T) {
		_, err := service.IssueCoupon(ctx, connect.NewRequest(&coupon.IssueCouponRequest{
			CampaignId: "non-existent-id",
		}))
		require.Error(t, err)
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("inactive campaign", func(t *testing.T) {
		// Update campaign status to finished
		_, err := service.pool.Exec(ctx,
			`UPDATE campaigns SET status = 'finished' WHERE id = $1`,
			campaignID,
		)
		require.NoError(t, err)

		_, err = service.IssueCoupon(ctx, connect.NewRequest(&coupon.IssueCouponRequest{
			CampaignId: campaignID,
		}))
		require.Error(t, err)
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

		// Reset status back to active
		_, err = service.pool.Exec(ctx,
			`UPDATE campaigns SET status = 'active' WHERE id = $1`,
			campaignID,
		)
		require.NoError(t, err)
	})

	t.Run("coupon limit reached", func(t *testing.T) {
		// Set counter to 0
		err := service.redis.Set(ctx, counterKey, 0, 0).Err()
		require.NoError(t, err)

		_, err = service.IssueCoupon(ctx, connect.NewRequest(&coupon.IssueCouponRequest{
			CampaignId: campaignID,
		}))
		require.Error(t, err)
		assert.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	})

	t.Run("last coupon updates campaign status", func(t *testing.T) {
		// Reset counter to 1 (last coupon)
		err := service.redis.Set(ctx, counterKey, 1, 0).Err()
		require.NoError(t, err)

		resp, err := service.IssueCoupon(ctx, connect.NewRequest(&coupon.IssueCouponRequest{
			CampaignId: campaignID,
		}))
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Msg.CouponCode)

		// Verify campaign status is updated to finished
		var status string
		err = service.pool.QueryRow(ctx,
			"SELECT status FROM campaigns WHERE id = $1",
			campaignID,
		).Scan(&status)
		require.NoError(t, err)
		assert.Equal(t, "finished", status)

		// Try to issue another coupon
		_, err = service.IssueCoupon(ctx, connect.NewRequest(&coupon.IssueCouponRequest{
			CampaignId: campaignID,
		}))
		require.Error(t, err)
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

		// Reset campaign status back to active for the next test
		_, err = service.pool.Exec(ctx,
			`UPDATE campaigns SET status = 'active' WHERE id = $1`,
			campaignID,
		)
		require.NoError(t, err)
	})

	t.Run("concurrent coupon issuance", func(t *testing.T) {
		// Reset counter to 2
		err := service.redis.Set(ctx, counterKey, 2, 0).Err()
		require.NoError(t, err)

		// Try to issue 3 coupons concurrently
		results := make(chan error, 3)
		for i := 0; i < 3; i++ {
			go func() {
				_, err := service.IssueCoupon(ctx, connect.NewRequest(&coupon.IssueCouponRequest{
					CampaignId: campaignID,
				}))
				results <- err
			}()
		}

		// Collect results
		successCount := 0
		errorCount := 0
		for i := 0; i < 3; i++ {
			err := <-results
			if err == nil {
				successCount++
			} else {
				errorCount++
			}
		}

		assert.Equal(t, 2, successCount)
		assert.Equal(t, 1, errorCount)
	})
}
