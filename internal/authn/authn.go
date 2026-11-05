package authn

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var ErrUnauthorized = errors.New("unauthorized")

type Manager struct {
	secret []byte
	ttl    time.Duration
}

func New(secret string, ttl time.Duration) (*Manager, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT_SECRET must contain at least 32 bytes")
	}
	if ttl <= 0 || ttl > 24*time.Hour {
		return nil, errors.New("JWT_TTL must be between 0 and 24h")
	}
	return &Manager{secret: []byte(secret), ttl: ttl}, nil
}

func FromEnv() (*Manager, error) {
	ttl := time.Hour
	if value := os.Getenv("JWT_TTL"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return nil, errors.New("JWT_TTL must be a duration, for example 1h")
		}
		ttl = parsed
	}
	return New(os.Getenv("JWT_SECRET"), ttl)
}

func (m *Manager) Issue(userID int64) (string, time.Time, error) {
	if userID <= 0 {
		return "", time.Time{}, ErrUnauthorized
	}
	now := time.Now()
	expires := now.Add(m.ttl).Truncate(time.Second)
	claims := jwt.RegisteredClaims{
		Issuer: "cashcraft", Subject: strconv.FormatInt(userID, 10),
		Audience: jwt.ClaimStrings{"cashcraft-api"},
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expires),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	return token, expires, err
}

func (m *Manager) Verify(raw string) (int64, error) {
	if raw == "" || len(raw) > 8192 {
		return 0, ErrUnauthorized
	}
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("cashcraft"),
		jwt.WithAudience("cashcraft-api"), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !token.Valid {
		return 0, ErrUnauthorized
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrUnauthorized
	}
	return id, nil
}

func Bearer(header string) (string, error) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", ErrUnauthorized
	}
	return parts[1], nil
}

type userKey struct{}

func WithUser(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userKey{}, userID)
}

func UserID(ctx context.Context) (int64, error) {
	id, ok := ctx.Value(userKey{}).(int64)
	if !ok || id <= 0 {
		return 0, ErrUnauthorized
	}
	return id, nil
}

func UnaryServerInterceptor(manager *Manager) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		values := md.Get("authorization")
		if len(values) != 1 {
			return nil, status.Error(codes.Unauthenticated, "unauthorized")
		}
		raw, err := Bearer(values[0])
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "unauthorized")
		}
		id, err := manager.Verify(raw)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "unauthorized")
		}
		return handler(WithUser(ctx, id), req)
	}
}
