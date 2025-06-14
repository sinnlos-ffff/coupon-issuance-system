package redis

import (
	"context"
	"fmt"

	"coupon-issuance/internal/utils"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Host     string
	Port     string
	Password string
	DB       int
}

func NewConfig() *Config {
	return &Config{
		Host:     utils.GetEnv("REDIS_HOST", "redis"),
		Port:     utils.GetEnv("REDIS_PORT", "6379"),
		Password: utils.GetEnv("REDIS_PASSWORD", ""),
		DB:       0, // use default DB
	}
}

func NewClient(cfg *Config) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
		Network:  "tcp4", // Force IPv4
	})

	// health check
	ctx := context.Background()
	err := client.Ping(ctx).Err()

	return client, err
}
