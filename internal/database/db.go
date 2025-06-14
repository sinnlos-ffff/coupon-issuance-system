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
		utils.GetEnv("DB_USER", "dummy_user"),
		utils.GetEnv("DB_PASSWORD", "dummy_password"),
		utils.GetEnv("DB_HOST", "localhost"),
		utils.GetEnv("DB_PORT", "5432"),
		utils.GetEnv("DB_NAME", "coupon_db"),
		utils.GetEnv("DB_SSL_MODE", "disable"),
	)

	pool, err := pgxpool.New(ctx, connString)

	return pool, err
}
