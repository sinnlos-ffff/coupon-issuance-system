package database

import (
	"context"
	"fmt"

	"coupon-issuance/internal/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context) (*pgxpool.Pool, error) {
	connString := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		utils.GetEnv("POSTGRES_USER", "dummy_user"),
		utils.GetEnv("POSTGRES_PASSWORD", "dummy_password"),
		utils.GetEnv("POSTGRES_HOST", "postgres"),
		utils.GetEnv("POSTGRES_PORT", "5432"),
		utils.GetEnv("POSTGRES_DB", "coupon_db"),
		utils.GetEnv("POSTGRES_SSL_MODE", "disable"),
	)

	pool, err := pgxpool.New(ctx, connString)

	return pool, err
}
