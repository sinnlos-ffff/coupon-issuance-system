package server

import (
	"context"
	"coupon-issuance/internal/database"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) (*pgxpool.Pool, string) {
	// TODO: separate test database
	ctx := context.Background()

	pool, err := database.NewPool(ctx)
	require.NoError(t, err)

	// clean up database
	_, err = pool.Exec(ctx, "DELETE FROM coupons")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "DELETE FROM campaigns")
	require.NoError(t, err)

	campaignID := "00000000-0000-0000-0000-000000000000"
	_, err = pool.Exec(ctx,
		`INSERT INTO campaigns (id, name, start_time, coupon_limit, status)
		VALUES ($1, $2, $3, $4, $5)`,
		campaignID,
		"Test Campaign",
		time.Now(),
		1000,
		"active",
	)
	require.NoError(t, err)

	return pool, campaignID
}

func TestCodeGenerator_GenerateCouponCode(t *testing.T) {
	pool, _ := setupTestDB(t)
	generator := newCodeGenerator()
	ctx := context.Background()
	campaignID := "00000000-0000-0000-0000-000000000000"

	// Test generating a single code
	code, err := generator.GenerateCouponCode(ctx, pool, campaignID)
	require.NoError(t, err)
	assert.NotEmpty(t, code)
	assert.Equal(t, len([]rune(code)), 10)

	// Test code uniqueness
	codes := make(map[string]bool)
	for i := 0; i < 100; i++ {
		code, err := generator.GenerateCouponCode(ctx, pool, campaignID)
		require.NoError(t, err)
		assert.False(t, codes[code], "Generated duplicate code: %s", code)
		codes[code] = true
	}
}

func TestCodeGenerator_ConcurrentAccess(t *testing.T) {
	pool, _ := setupTestDB(t)
	generator := newCodeGenerator()
	ctx := context.Background()
	campaignID := "00000000-0000-0000-0000-000000000000"
	codes := make(chan string, 1000)
	errors := make(chan error, 1000)

	// Generate codes concurrently
	for i := 0; i < 1000; i++ {
		go func() {
			code, err := generator.GenerateCouponCode(ctx, pool, campaignID)
			if err != nil {
				errors <- err
				return
			}
			codes <- code
		}()
	}

	// Collect results
	generatedCodes := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		select {
		case err := <-errors:
			t.Fatalf("Error generating code: %v", err)
		case code := <-codes:
			assert.False(t, generatedCodes[code], "Generated duplicate code: %s", code)
			generatedCodes[code] = true
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for code generation")
		}
	}
}

func TestCodeGenerator_WriteIssuedCodes(t *testing.T) {
	pool, campaignID := setupTestDB(t)
	generator := newCodeGenerator()
	ctx := context.Background()

	codes := make([]string, 10)
	for i := 0; i < 10; i++ {
		code, err := generator.GenerateCouponCode(ctx, pool, campaignID)
		require.NoError(t, err)
		codes[i] = code
	}

	err := generator.writeIssuedCodes(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, len(generator.usedCodes), 0)

	for _, code := range codes {
		var exists bool
		err := pool.QueryRow(
			ctx,
			"SELECT EXISTS(SELECT 1 FROM coupons WHERE code = $1 AND campaign_id = $2)",
			code,
			campaignID,
		).Scan(&exists)
		require.NoError(t, err)
		assert.True(t, exists)
	}
}

func TestCodeGenerator_RefillPool(t *testing.T) {
	pool, _ := setupTestDB(t)
	generator := newCodeGenerator()
	ctx := context.Background()
	campaignID := "00000000-0000-0000-0000-000000000000"

	// Test initial pool refill
	err := generator.refillPool(ctx, pool)
	require.NoError(t, err)
	assert.Greater(t, len(generator.codePool), generator.batchSize/4)

	// Test pool refill after using some codes
	for i := 0; i < len(generator.codePool); i++ {
		go func() {
			_, err := generator.GenerateCouponCode(ctx, pool, campaignID)
			require.NoError(t, err)
		}()
	}

	// Wait for background refill
	time.Sleep(100 * time.Millisecond)
	assert.Greater(t, len(generator.codePool), generator.batchSize/4)
}
