package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/mishamarkov15/final-hw-2026-dec/services/auth/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

type Repository interface {
	Create(context.Context, string, string) (domain.User, error)
	ByEmail(context.Context, string) (domain.User, error)
}

type TokenIssuer interface {
	Issue(int64) (string, time.Time, error)
}

type Result struct {
	User        domain.User
	AccessToken string
	ExpiresAt   time.Time
}

type Service struct {
	repo      Repository
	tokens    TokenIssuer
	slots     chan struct{}
	dummyHash []byte
}

func New(repo Repository, tokens TokenIssuer) (*Service, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte("invalid-account-password"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Service{repo: repo, tokens: tokens, slots: make(chan struct{}, 8), dummyHash: hash}, nil
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if len(email) == 0 || len(email) > 254 {
		return "", fmt.Errorf("%w: email must be a valid address", domain.ErrInvalidInput)
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", fmt.Errorf("%w: email must be a valid address", domain.ErrInvalidInput)
	}
	return email, nil
}

func (s *Service) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return domain.ErrBusy
	}
}

func (s *Service) Register(ctx context.Context, email, password string) (Result, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return Result{}, err
	}
	if len(password) < 8 || len(password) > 72 {
		return Result{}, fmt.Errorf("%w: password must contain 8 to 72 bytes", domain.ErrInvalidInput)
	}
	if err := s.acquire(ctx); err != nil {
		return Result{}, err
	}
	defer func() { <-s.slots }()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return Result{}, fmt.Errorf("hash password: %w", err)
	}
	u, err := s.repo.Create(ctx, email, string(hash))
	if err != nil {
		return Result{}, err
	}
	return s.result(u)
}

func (s *Service) Login(ctx context.Context, email, password string) (Result, error) {
	email, err := normalizeEmail(email)
	if err != nil || len(password) < 8 || len(password) > 72 {
		return Result{}, domain.ErrInvalidCredentials
	}
	if err := s.acquire(ctx); err != nil {
		return Result{}, err
	}
	defer func() { <-s.slots }()
	u, err := s.repo.ByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) {
			_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		}
		return Result{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return Result{}, domain.ErrInvalidCredentials
	}
	return s.result(u)
}

func (s *Service) result(u domain.User) (Result, error) {
	token, expires, err := s.tokens.Issue(u.ID)
	if err != nil {
		return Result{}, err
	}
	u.PasswordHash = ""
	return Result{User: u, AccessToken: token, ExpiresAt: expires}, nil
}
