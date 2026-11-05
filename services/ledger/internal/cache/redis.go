// Package cache — кеш «ключ-значение» с TTL поверх Redis. Значения хранятся как JSON.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const pingTimeout = 5 * time.Second

type Config struct {
	Addr     string
	Password string
	DB       int
}

// ConfigFromEnv читает REDIS_ADDR (по умолчанию localhost:6379),
// REDIS_DB (по умолчанию 0) и REDIS_PASSWORD (необязательна).
func ConfigFromEnv() (Config, error) {
	cfg := Config{Addr: "localhost:6379", Password: os.Getenv("REDIS_PASSWORD")}
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		cfg.Addr = addr
	}
	if value := os.Getenv("REDIS_DB"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return Config{}, fmt.Errorf("REDIS_DB должна быть неотрицательным целым числом, получено %q", value)
		}
		cfg.DB = n
	}
	return cfg, nil
}

type Cache struct {
	client *redis.Client
}

// New подключается к Redis и проверяет соединение через PING.
func New(ctx context.Context, cfg Config) (*Cache, error) {
	// Если Redis недоступен, запрос должен быстро уйти в БД, а не ждать
	// повторных попыток: короткие таймауты и одна попытка без повторов.
	client := redis.NewClient(&redis.Options{
		Addr:                  cfg.Addr,
		Password:              cfg.Password,
		DB:                    cfg.DB,
		DialTimeout:           time.Second,
		ReadTimeout:           500 * time.Millisecond,
		WriteTimeout:          500 * time.Millisecond,
		MaxRetries:            -1,
		DialerRetries:         1,
		ContextTimeoutEnabled: true,
	})

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("%s, db %d: %w", cfg.Addr, cfg.DB, err)
	}

	log.Printf("Redis: подключение установлено (%s, db %d)", cfg.Addr, cfg.DB)
	return &Cache{client: client}, nil
}

// GetJSON читает значение по ключу и разбирает его в dst.
// Возвращает false без ошибки, если ключа нет или его TTL истёк.
func (c *Cache) GetJSON(ctx context.Context, key string, dst any) (bool, error) {
	data, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return false, fmt.Errorf("разбор значения %q: %w", key, err)
	}
	return true, nil
}

// SetJSON сохраняет значение в виде JSON на время ttl.
func (c *Cache) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("сериализация значения %q: %w", key, err)
	}
	return c.client.Set(ctx, key, data, ttl).Err()
}

func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

// DeleteByPrefix удаляет все ключи, начинающиеся с prefix. Ключи ищутся
// через SCAN, поэтому Redis не блокируется, как при KEYS.
func (c *Cache) DeleteByPrefix(ctx context.Context, prefix string) error {
	iter := c.client.Scan(ctx, 0, prefix+"*", 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	return c.client.Del(ctx, keys...).Err()
}

func (c *Cache) Close() error {
	return c.client.Close()
}
