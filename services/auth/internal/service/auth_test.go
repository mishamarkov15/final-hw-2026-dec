package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mishamarkov15/final-hw-2026-dec/services/auth/internal/domain"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

type memoryUsers struct{ users map[string]domain.User }

func (r *memoryUsers) Create(_ context.Context, email, hash string) (domain.User, error) {
	if _, ok := r.users[email]; ok {
		return domain.User{}, domain.ErrEmailExists
	}
	u := domain.User{ID: int64(len(r.users) + 1), Email: email, PasswordHash: hash}
	r.users[email] = u
	return u, nil
}

func (r *memoryUsers) ByEmail(_ context.Context, email string) (domain.User, error) {
	u, ok := r.users[email]
	if !ok {
		return domain.User{}, domain.ErrInvalidCredentials
	}
	return u, nil
}

type issuer struct{}

func (issuer) Issue(id int64) (string, time.Time, error) {
	return "signed-token", time.Now().Add(time.Hour), nil
}

func TestRegistrationAndLogin(t *testing.T) {
	repo := &memoryUsers{users: make(map[string]domain.User)}
	s, err := New(repo, issuer{})
	require.NoError(t, err)
	ctx := context.Background()
	r, err := s.Register(ctx, "  Test@Example.com  ", "correct-password")
	require.NoError(t, err)
	require.Equal(t, "test@example.com", r.User.Email)
	require.Equal(t, int64(1), r.User.ID)
	require.Equal(t, "signed-token", r.AccessToken)
	require.Empty(t, r.User.PasswordHash)
	stored := repo.users["test@example.com"]
	require.NotEqual(t, "correct-password", stored.PasswordHash)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("correct-password")))
	_, err = s.Register(ctx, "TEST@example.com", "other-password")
	require.ErrorIs(t, err, domain.ErrEmailExists)
	logged, err := s.Login(ctx, " TEST@example.com ", "correct-password")
	require.NoError(t, err)
	require.Equal(t, r.User, logged.User)
	for _, email := range []string{"test@example.com", "missing@example.com", "invalid"} {
		_, err = s.Login(ctx, email, "wrong-password")
		require.ErrorIs(t, err, domain.ErrInvalidCredentials)
		require.Equal(t, "invalid email or password", err.Error())
	}
}

func TestInvalidRegistrationDoesNotWrite(t *testing.T) {
	repo := &memoryUsers{users: make(map[string]domain.User)}
	s, err := New(repo, issuer{})
	require.NoError(t, err)
	for _, tc := range []struct{ name, email, password string }{
		{"empty email", "", "password123"},
		{"malformed email", "not-an-email", "password123"},
		{"display name", "Test <test@example.com>", "password123"},
		{"email too long", strings.Repeat("a", 250) + "@example.com", "password123"},
		{"short password", "test@example.com", "1234567"},
		{"password too long", "test@example.com", strings.Repeat("a", 73)},
		{"password byte length", "test@example.com", strings.Repeat("я", 37)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Register(context.Background(), tc.email, tc.password)
			require.ErrorIs(t, err, domain.ErrInvalidInput)
		})
	}
	require.Empty(t, repo.users)
}

func TestBoundedAuthenticationAndCanceledRequests(t *testing.T) {
	repo := &memoryUsers{users: make(map[string]domain.User)}
	s, err := New(repo, issuer{})
	require.NoError(t, err)
	for i := 0; i < cap(s.slots); i++ {
		s.slots <- struct{}{}
	}
	_, err = s.Register(context.Background(), "a@example.com", "password123")
	require.ErrorIs(t, err, domain.ErrBusy)
	_, err = s.Login(context.Background(), "a@example.com", "password123")
	require.ErrorIs(t, err, domain.ErrBusy)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Register(ctx, "a@example.com", "password123")
	require.True(t, errors.Is(err, context.Canceled))
	require.Empty(t, repo.users)
}
