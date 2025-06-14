package server

import (
	"context"
	"fmt"
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

	// Prepare batch insert
	values := make([]string, len(codes))
	args := make([]interface{}, len(codes)*2)
	for i := range codes {
		values[i] = fmt.Sprintf("($%d, $%d)", i*2+1, i*2+2)
		args[i*2] = codes[i]
		var campaignID pgtype.UUID
		err := campaignID.Scan(campaignIDs[i])
		if err != nil {
			return fmt.Errorf("failed to parse campaign ID: %w", err)
		}
		args[i*2+1] = campaignID
	}

	query := fmt.Sprintf(`
		INSERT INTO coupons (code, campaign_id)
		VALUES %s`,
		strings.Join(values, ","))

	_, err := pool.Exec(ctx, query, args...)
	if err != nil {
		// If write fails, put the codes back in usedCodes
		g.mu.Lock()
		for i := range codes {
			g.usedCoupons[codes[i]] = campaignIDs[i]
		}
		g.mu.Unlock()
		return fmt.Errorf("failed to write used codes: %w", err)
	}

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

	placeholders := make([]string, len(codes))
	args := make([]interface{}, len(codes))
	for i, code := range codes {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = code
	}

	query := fmt.Sprintf(`
		SELECT code 
		FROM coupons 
		WHERE code IN (%s)`,
		strings.Join(placeholders, ","))

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to check coupon codes: %w", err)
	}
	defer rows.Close()

	existingCodes := make(map[string]struct{})
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return fmt.Errorf("failed to scan existing code: %w", err)
		}
		existingCodes[code] = struct{}{}
	}

	for _, code := range codes {
		if _, exists := existingCodes[code]; !exists {
			g.codePool = append(g.codePool, code)
		}
	}

	return nil
}

func (g *codeGenerator) GenerateCouponCode(
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
