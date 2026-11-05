package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mishamarkov15/final-hw-2026-dec/services/auth/internal/domain"
)

type Postgres struct{ db *sql.DB }

func NewPostgres(db *sql.DB) *Postgres { return &Postgres{db: db} }

func (r *Postgres) Create(ctx context.Context, email, hash string) (domain.User, error) {
	var u domain.User
	err := r.db.QueryRowContext(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id, email, password_hash`, email, hash).Scan(&u.ID, &u.Email, &u.PasswordHash)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.User{}, domain.ErrEmailExists
	}
	return u, err
}

func (r *Postgres) ByEmail(ctx context.Context, email string) (domain.User, error) {
	var u domain.User
	err := r.db.QueryRowContext(ctx, `SELECT id, email, password_hash FROM users WHERE email = $1`, email).Scan(&u.ID, &u.Email, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrInvalidCredentials
	}
	return u, err
}
