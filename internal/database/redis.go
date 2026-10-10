package database

import (
	"context"

	"github.com/redis/go-redis/v9"
)

func NewRedisClient(addr, password string, DB int) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       DB,
	})

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		panic(err)
	}

	return rdb
}
