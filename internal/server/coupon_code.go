package server

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	koreanStart = 0xAC00 // 가
	koreanEnd   = 0xD7A3 // 힣
	numberStart = 0x0030 // 0
	numberEnd   = 0x0039 // 9
)

type codeGenerator struct {
	mu          sync.Mutex
	codePool    []string
	usedCoupons map[string]string // map of code to campaign ID
	batchSize   int
}

func newCodeGenerator() *codeGenerator {
	batchSize := 1000
	return &codeGenerator{
		batchSize:   batchSize,
		codePool:    make([]string, 0, batchSize),
		usedCoupons: make(map[string]string, batchSize),
	}
}

func generateRandomKoreanChar() rune {
	return rune(koreanStart + rand.Intn(koreanEnd-koreanStart+1))
}

func generateRandomNumber() rune {
	return rune(numberStart + rand.Intn(numberEnd-numberStart+1))
}

func (g *codeGenerator) generateBatch() []string {
	length := 10
	codes := make([]string, g.batchSize)
	for i := 0; i < g.batchSize; i++ {
		code := make([]rune, length)
		for j := 0; j < length; j++ {
			if rand.Float32() < 0.5 {
				code[j] = generateRandomKoreanChar()
			} else {
				code[j] = generateRandomNumber()
			}
		}
		codes[i] = string(code)
	}
	return codes
}

func (g *codeGenerator) writeIssuedCodes(
	ctx context.Context,
	pool *pgxpool.Pool,
) error {
	g.mu.Lock()
	if len(g.usedCoupons) == 0 {
		g.mu.Unlock()
		return nil
	}

	// Take a copy of used codes and clear the map
	codes := make([]string, 0, len(g.usedCoupons))
	campaignIDs := make([]string, 0, len(g.usedCoupons))
	for code, campaignID := range g.usedCoupons {
		codes = append(codes, code)
		campaignIDs = append(campaignIDs, campaignID)
	}
	g.usedCoupons = make(map[string]string)
	g.mu.Unlock()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
				log.Printf("failed to rollback transaction: %v", rollbackErr)
			}
		}
	}()

	// Update the codes with campaign_id and mark as issued
	placeholders := make([]string, len(codes))
	args := make([]interface{}, len(codes)*2)
	for i := range codes {
		placeholders[i] = fmt.Sprintf("($%d, $%d)", i*2+1, i*2+2)
		args[i*2] = codes[i]
		var campaignID pgtype.UUID
		err := campaignID.Scan(campaignIDs[i])
		if err != nil {
			return fmt.Errorf("failed to parse campaign ID: %w", err)
		}
		args[i*2+1] = campaignID
	}

	query := fmt.Sprintf(`
		WITH input_codes(code, campaign_id) AS (
			VALUES %s
		)
		UPDATE coupons c
		SET campaign_id = i.campaign_id::uuid, issued = TRUE
		FROM input_codes i
		WHERE c.code = i.code 
		AND c.campaign_id IS NULL 
		AND c.issued = FALSE
		RETURNING c.code`, strings.Join(placeholders, ","))

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		// If write fails, put the codes back in usedCodes
		g.mu.Lock()
		for i := range codes {
			g.usedCoupons[codes[i]] = campaignIDs[i]
		}
		g.mu.Unlock()
		return fmt.Errorf("failed to write used codes: %w", err)
	}
	defer rows.Close()

	// Verify all codes were updated
	updatedCodes := make(map[string]struct{})
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return fmt.Errorf("failed to scan updated code: %w", err)
		}
		updatedCodes[code] = struct{}{}
	}

	// Check if any codes failed to update
	for _, code := range codes {
		if _, updated := updatedCodes[code]; !updated {
			// Put failed codes back in usedCodes
			g.mu.Lock()
			for i, c := range codes {
				if c == code {
					g.usedCoupons[code] = campaignIDs[i]
					break
				}
			}
			g.mu.Unlock()
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	tx = nil // Set tx to nil after successful commit

	return nil
}

func (g *codeGenerator) refillPool(
	ctx context.Context,
	pool *pgxpool.Pool,
) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.codePool) > g.batchSize/4 {
		return nil
	}

	codes := g.generateBatch()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
				log.Printf("failed to rollback transaction: %v", rollbackErr)
			}
		}
	}()

	// Insert unissued codes into coupons table
	placeholders := make([]string, len(codes))
	args := make([]interface{}, len(codes))
	for i, code := range codes {
		placeholders[i] = fmt.Sprintf("($%d)", i+1)
		args[i] = code
	}

	query := fmt.Sprintf(`
		WITH inserted_codes AS (
			INSERT INTO coupons (code)
			VALUES %s
			ON CONFLICT (code) DO NOTHING
			RETURNING code
		)
		SELECT code FROM inserted_codes`,
		strings.Join(placeholders, ","))

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to check reserved codes: %w", err)
	}
	defer rows.Close()

	// Add successfully reserved codes to the pool
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return fmt.Errorf("failed to scan reserved code: %w", err)
		}
		g.codePool = append(g.codePool, code)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	tx = nil // Set tx to nil after successful commit

	return nil
}

func (g *codeGenerator) generateCouponCode(
	ctx context.Context,
	pool *pgxpool.Pool,
	campaignID string,
) (string, error) {
	if err := g.refillPool(ctx, pool); err != nil {
		return "", err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	code := g.codePool[0]
	g.codePool = g.codePool[1:]
	g.usedCoupons[code] = campaignID

	return code, nil
}

func (g *codeGenerator) hasPendingCodes() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.usedCoupons) > 0
}
