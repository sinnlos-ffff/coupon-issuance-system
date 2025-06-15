package server

import (
	"context"
	"testing"
	"time"

	coupon "coupon-issuance/gen/coupon/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCouponService_HighThroughputLoad(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()

	// Create a test campaign with high limit
	resp, err := service.CreateCampaign(ctx, connect.NewRequest(&coupon.CreateCampaignRequest{
		Name:        "Load Test Campaign",
		StartTime:   time.Now().Format(time.RFC3339),
		CouponLimit: 3000,
	}))
	require.NoError(t, err)
	campaignID := resp.Msg.CampaignId

	// Wait for campaign to be activated by the worker
	time.Sleep(2 * time.Second)

	// Verify campaign is active
	var status string
	err = service.pool.QueryRow(ctx,
		"SELECT status FROM campaigns WHERE id = $1",
		campaignID,
	).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "active", status, "Campaign should be active")

	duration := 5 * time.Second
	targetRate := 1000 // coupons per second
	interval := time.Second / time.Duration(targetRate)

	// Start time
	start := time.Now()
	issued := 0
	errors := 0
	done := make(chan struct{})

	// Start issuing coupons
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_, err := service.IssueCoupon(ctx, connect.NewRequest(&coupon.IssueCouponRequest{
					CampaignId: campaignID,
				}))
				if err != nil {
					errors++
				} else {
					issued++
				}
			}
		}
	}()

	time.Sleep(duration)
	close(done)

	elapsed := time.Since(start)
	actualRate := float64(issued) / elapsed.Seconds()

	t.Logf("Issued %d coupons in %v (%.2f/sec)", issued, elapsed, actualRate)
	t.Logf("Encountered %d errors", errors)

	// Allow some time for background writer to flush
	time.Sleep(2 * time.Second)

	// Verify all codes were written to database
	var count int
	err = service.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM coupons WHERE campaign_id = $1",
		campaignID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, issued, count)
}

func TestCouponService_MultiCampaignConcurrency(t *testing.T) {
	service := setupTestService(t)
	ctx := context.Background()

	// Create multiple test campaigns
	campaigns := []string{}

	for i := 0; i < 3; i++ {
		resp, err := service.CreateCampaign(ctx, connect.NewRequest(&coupon.CreateCampaignRequest{
			Name:        "Load Test Campaign",
			StartTime:   time.Now().Format(time.RFC3339),
			CouponLimit: 10000,
		}))
		require.NoError(t, err)
		campaigns = append(campaigns, resp.Msg.CampaignId)
	}

	// Wait for campaign to be activated by the worker
	time.Sleep(2 * time.Second)

	results := make(chan struct {
		campaignID string
		err        error
	}, 3000)

	for _, campaignID := range campaigns {
		for i := 0; i < 1000; i++ {
			go func(cid string) {
				_, err := service.IssueCoupon(ctx, connect.NewRequest(&coupon.IssueCouponRequest{
					CampaignId: cid,
				}))
				results <- struct {
					campaignID string
					err        error
				}{cid, err}
			}(campaignID)
		}
	}

	// Collect results
	successCount := make(map[string]int)
	errorCount := make(map[string]int)
	for i := 0; i < 3000; i++ {
		result := <-results
		if result.err == nil {
			successCount[result.campaignID]++
		} else {
			errorCount[result.campaignID]++
		}
	}

	// Allow time for background writer
	time.Sleep(2 * time.Second)

	// Verify results
	for _, campaignID := range campaigns {
		t.Logf("Campaign %s: %d successful, %d errors",
			campaignID, successCount[campaignID], errorCount[campaignID])

		assert.True(t, successCount[campaignID] == 1000)
		assert.True(t, errorCount[campaignID] == 0)

		// Verify database count
		var count int
		err := service.pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM coupons WHERE campaign_id = $1",
			campaignID,
		).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, successCount[campaignID], count)
	}
}
