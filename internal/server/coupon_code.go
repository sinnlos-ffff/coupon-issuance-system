package server

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	koreanStart = 0xAC00 // 가
	koreanEnd   = 0xD7A3 // 힣
	numberStart = 0x0030 // 0
	numberEnd   = 0x0039 // 9
)

type codeGenerator struct {
	mu        sync.Mutex
	codePool  []string
	usedCodes []string
	batchSize int
}

func newCodeGenerator() *codeGenerator {
	batchSize := 1000
	return &codeGenerator{
		batchSize: batchSize,
		codePool:  make([]string, 0, batchSize),
		usedCodes: make([]string, 0, batchSize),
	}
}

func generateRandomKoreanChar() rune {
	return rune(koreanStart + rand.Intn(koreanEnd-koreanStart+1))
}

func generateRandomNumber() rune {
	return rune(numberStart + rand.Intn(numberEnd-numberStart+1))
}

func (g *codeGenerator) generateBatch(length int) []string {
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

func (g *codeGenerator) writeIssuedCodes(ctx context.Context, pool *pgxpool.Pool) error {
	g.mu.Lock()
	if len(g.usedCodes) == 0 {
		g.mu.Unlock()
		return nil
	}

	// Take a copy of used codes and clear the slice
	codes := make([]string, len(g.usedCodes))
	copy(codes, g.usedCodes)
	g.usedCodes = g.usedCodes[:0]
	g.mu.Unlock()

	// Prepare batch insert
	values := make([]string, len(codes))
	args := make([]interface{}, len(codes))
	for i, code := range codes {
		values[i] = fmt.Sprintf("($%d)", i+1)
		args[i] = code
	}

	query := fmt.Sprintf(`
		INSERT INTO coupons (code, created_at)
		VALUES %s`,
		strings.Join(values, ","))

	_, err := pool.Exec(ctx, query, args...)
	if err != nil {
		// If write fails, put the codes back in usedCodes
		g.mu.Lock()
		g.usedCodes = append(g.usedCodes, codes...)
		g.mu.Unlock()
		return fmt.Errorf("failed to write used codes: %w", err)
	}

	return nil
}

func (g *codeGenerator) refillPool(ctx context.Context, pool *pgxpool.Pool) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.codePool) > g.batchSize/4 {
		return nil
	}

	codes := g.generateBatch(g.batchSize)

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

func (g *codeGenerator) GenerateCouponCode(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	if err := g.refillPool(ctx, pool); err != nil {
		log.Printf("Failed to refill code pool: %v", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	code := g.codePool[0]
	g.codePool = g.codePool[1:]
	g.usedCodes = append(g.usedCodes, code)

	return code, nil
}
