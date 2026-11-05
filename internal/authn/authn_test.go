package authn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const testSecret = "unit-test-secret-not-for-production-123456789"

func TestTokenRoundTrip(t *testing.T) {
	m, err := New(testSecret, time.Hour)
	require.NoError(t, err)
	raw, expires, err := m.Issue(42)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(time.Hour), expires, 2*time.Second)
	id, err := m.Verify(raw)
	require.NoError(t, err)
	require.Equal(t, int64(42), id)
	_, err = m.Verify(raw + "tampered")
	require.ErrorIs(t, err, ErrUnauthorized)
	_, _, err = m.Issue(0)
	require.Error(t, err)
}

func TestInvalidClaims(t *testing.T) {
	m, err := New(testSecret, time.Hour)
	require.NoError(t, err)
	for _, name := range []string{"expired", "missing_exp", "issuer", "audience", "subject", "future", "algorithm", "secret"} {
		t.Run(name, func(t *testing.T) {
			c := jwt.RegisteredClaims{Issuer: "cashcraft", Subject: "1", Audience: jwt.ClaimStrings{"cashcraft-api"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}
			var method jwt.SigningMethod = jwt.SigningMethodHS256
			key := []byte(testSecret)
			switch name {
			case "expired":
				c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
			case "missing_exp":
				c.ExpiresAt = nil
			case "issuer":
				c.Issuer = "someone-else"
			case "audience":
				c.Audience = jwt.ClaimStrings{"other"}
			case "subject":
				c.Subject = "-1"
			case "future":
				c.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
			case "algorithm":
				method = jwt.SigningMethodHS512
			case "secret":
				key = []byte(strings.Repeat("x", 32))
			}
			raw, err := jwt.NewWithClaims(method, c).SignedString(key)
			require.NoError(t, err)
			_, err = m.Verify(raw)
			require.ErrorIs(t, err, ErrUnauthorized)
		})
	}
}

func TestInterceptorUsesVerifiedIdentity(t *testing.T) {
	m, err := New(testSecret, time.Hour)
	require.NoError(t, err)
	raw, _, err := m.Issue(7)
	require.NoError(t, err)
	interceptor := UnaryServerInterceptor(m)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+raw, "x-user-id", "999"))
	result, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, _ any) (any, error) { return UserID(ctx) })
	require.NoError(t, err)
	require.Equal(t, int64(7), result)
	_, err = interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) { t.Fatal("must not be called"); return nil, nil })
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+raw, "authorization", "Bearer "+raw))
	_, err = interceptor(ctx, nil, &grpc.UnaryServerInfo{}, nil)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}
