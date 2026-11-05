package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"time"

	"github.com/mishamarkov15/final-hw-2026-dec/services/ledger/internal/domain"
)

type queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct {
	db    *sql.DB
	query queryer
}

func New(db *sql.DB) *Store { return &Store{db: db, query: db} }

var _ domain.Repository = (*Store)(nil)
var _ domain.TransactionManager = (*Store)(nil)

func (s *Store) WithCategory(ctx context.Context, uid int64, category string, fn func(domain.Repository) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	// Every writer acquires the same transaction-scoped lock, including when a
	// category has no budget row yet. Hash collisions only serialize extra work.
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(strconv.FormatInt(uid, 10) + ":" + category))
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", int64(hash.Sum64())); err != nil {
		return fmt.Errorf("lock category: %w", err)
	}
	if err := fn(&Store{query: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
func (s *Store) SetBudget(ctx context.Context, uid int64, b domain.Budget) error {
	_, err := s.query.ExecContext(ctx, `INSERT INTO budgets(user_id, category, limit_amount) VALUES ($1,$2,$3)
  ON CONFLICT(user_id, category) DO UPDATE SET limit_amount=EXCLUDED.limit_amount`, uid, b.Category, fmt.Sprintf("%.2f", b.Limit))
	return err
}
func (s *Store) GetBudget(ctx context.Context, uid int64, category string) (domain.Budget, error) {
	var b domain.Budget
	err := s.query.QueryRowContext(ctx, `SELECT category, limit_amount FROM budgets WHERE user_id=$1 AND category=$2`, uid, category).Scan(&b.Category, &b.Limit)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Budget{}, domain.ErrNotFound
	}
	return b, err
}
func (s *Store) ListBudgets(ctx context.Context, uid int64) ([]domain.Budget, error) {
	rows, err := s.query.QueryContext(ctx, `SELECT category, limit_amount FROM budgets WHERE user_id=$1 ORDER BY category`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Budget, 0)
	for rows.Next() {
		var item domain.Budget
		if err := rows.Scan(&item.Category, &item.Limit); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *Store) AddTransaction(ctx context.Context, uid int64, item domain.Transaction) (domain.Transaction, error) {
	err := s.query.QueryRowContext(ctx, `INSERT INTO expenses(user_id, amount, category, description, date)
  VALUES ($1,$2,$3,$4,$5) RETURNING id`, uid, fmt.Sprintf("%.2f", item.Amount), item.Category, item.Description, item.Date).Scan(&item.ID)
	return item, err
}
func (s *Store) ListTransactions(ctx context.Context, uid int64) ([]domain.Transaction, error) {
	rows, err := s.query.QueryContext(ctx, `SELECT id,amount,category,description,date FROM expenses WHERE user_id=$1 ORDER BY date DESC,id DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Transaction, 0)
	for rows.Next() {
		var item domain.Transaction
		var date time.Time
		if err := rows.Scan(&item.ID, &item.Amount, &item.Category, &item.Description, &date); err != nil {
			return nil, err
		}
		item.Date = date.Format("2006-01-02")
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *Store) SumByCategory(ctx context.Context, uid int64, category string) (int64, error) {
	var cents int64
	err := s.query.QueryRowContext(ctx, `SELECT (COALESCE(SUM(amount),0)*100)::BIGINT FROM expenses WHERE user_id=$1 AND category=$2`, uid, category).Scan(&cents)
	return cents, err
}
func (s *Store) GetReportSummary(ctx context.Context, uid int64, from, to string) (domain.ReportSummary, error) {
	rows, err := s.query.QueryContext(ctx, `WITH spending AS (
   SELECT category, SUM(amount) AS amount FROM expenses
   WHERE user_id=$1 AND date BETWEEN $2 AND $3 GROUP BY category
  ), limits AS (SELECT category,limit_amount FROM budgets WHERE user_id=$1)
  SELECT COALESCE(spending.category,limits.category), (COALESCE(spending.amount,0)*100)::BIGINT, limits.limit_amount
  FROM spending FULL OUTER JOIN limits USING(category) ORDER BY 1`, uid, from, to)
	if err != nil {
		return domain.ReportSummary{}, err
	}
	defer rows.Close()
	result := domain.ReportSummary{From: from, To: to, Categories: make([]domain.CategoryTotal, 0)}
	var total int64
	for rows.Next() {
		var item domain.CategoryTotal
		var cents int64
		var limit sql.NullFloat64
		if err := rows.Scan(&item.Category, &cents, &limit); err != nil {
			return domain.ReportSummary{}, err
		}
		item.Total = domain.FromCents(cents)
		item.HasBudget = limit.Valid
		if limit.Valid {
			item.BudgetLimit = limit.Float64
			item.BudgetUsedPercent = item.Total / item.BudgetLimit * 100
		}
		total += cents
		result.Categories = append(result.Categories, item)
	}
	result.Total = domain.FromCents(total)
	return result, rows.Err()
}
