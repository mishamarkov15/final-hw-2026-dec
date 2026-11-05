package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mishamarkov15/final-hw-2026-dec/services/ledger/internal/domain"
)

const CacheTTL = 30 * time.Second

type Service struct {
	repo  domain.Repository
	tx    domain.TransactionManager
	cache domain.Cache
}

func New(repo domain.Repository, tx domain.TransactionManager, cache domain.Cache) *Service {
	if cache == nil {
		cache = noCache{}
	}
	return &Service{repo: repo, tx: tx, cache: cache}
}
func budgetsKey(uid int64) string   { return fmt.Sprintf("budgets:%d:all", uid) }
func reportPrefix(uid int64) string { return fmt.Sprintf("report:summary:%d:", uid) }

func (s *Service) SetBudget(ctx context.Context, uid int64, b domain.Budget) (domain.Budget, error) {
	b.Category = strings.TrimSpace(b.Category)
	if err := b.Validate(); err != nil {
		return domain.Budget{}, err
	}
	err := s.tx.WithCategory(ctx, uid, b.Category, func(repo domain.Repository) error {
		return repo.SetBudget(ctx, uid, b)
	})
	if err != nil {
		return domain.Budget{}, err
	}
	if err := s.cache.Delete(ctx, budgetsKey(uid)); err != nil {
		log.Printf("ledger: invalidate budgets: %v", err)
	}
	s.invalidateReports(ctx, uid)
	return b, nil
}

func (s *Service) ListBudgets(ctx context.Context, uid int64) ([]domain.Budget, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := budgetsKey(uid)
	var result []domain.Budget
	hit, err := s.cache.GetJSON(ctx, key, &result)
	if err != nil {
		log.Printf("ledger: cache read %s: %v", key, err)
	}
	if err == nil && hit {
		log.Printf("ledger: cache hit %s", key)
		return result, nil
	}
	log.Printf("ledger: cache miss %s", key)
	result, err = s.repo.ListBudgets(ctx, uid)
	if err != nil {
		return nil, err
	}
	if err := s.cache.SetJSON(ctx, key, result, CacheTTL); err != nil {
		log.Printf("ledger: cache write %s: %v", key, err)
	}
	return result, nil
}

func (s *Service) AddTransaction(ctx context.Context, uid int64, item domain.Transaction) (domain.Transaction, error) {
	item.Category = strings.TrimSpace(item.Category)
	item.ID = 0
	if err := item.Validate(); err != nil {
		return domain.Transaction{}, err
	}
	var saved domain.Transaction
	err := s.tx.WithCategory(ctx, uid, item.Category, func(repo domain.Repository) error {
		budget, err := repo.GetBudget(ctx, uid, item.Category)
		switch {
		case errors.Is(err, domain.ErrNotFound):
		case err != nil:
			return err
		default:
			spent, err := repo.SumByCategory(ctx, uid, item.Category)
			if err != nil {
				return err
			}
			// Compare cents without adding potentially overflowing totals.
			if spent > domain.ToCents(budget.Limit)-domain.ToCents(item.Amount) {
				return fmt.Errorf("%w: category %q", domain.ErrBudgetExceeded, item.Category)
			}
		}
		saved, err = repo.AddTransaction(ctx, uid, item)
		return err
	})
	if err != nil {
		return domain.Transaction{}, err
	}
	s.invalidateReports(ctx, uid)
	return saved, nil
}

func (s *Service) ListTransactions(ctx context.Context, uid int64) ([]domain.Transaction, error) {
	return s.repo.ListTransactions(ctx, uid)
}
func (s *Service) GetReportSummary(ctx context.Context, uid int64, from, to string) (domain.ReportSummary, error) {
	if err := (domain.Period{From: from, To: to}).Validate(); err != nil {
		return domain.ReportSummary{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.ReportSummary{}, err
	}
	key := reportPrefix(uid) + from + ":" + to
	var result domain.ReportSummary
	hit, err := s.cache.GetJSON(ctx, key, &result)
	if err != nil {
		log.Printf("ledger: cache read %s: %v", key, err)
	}
	if err == nil && hit {
		log.Printf("ledger: cache hit %s", key)
		return result, nil
	}
	log.Printf("ledger: cache miss %s", key)
	result, err = s.repo.GetReportSummary(ctx, uid, from, to)
	if err != nil {
		return domain.ReportSummary{}, err
	}
	if err := s.cache.SetJSON(ctx, key, result, CacheTTL); err != nil {
		log.Printf("ledger: cache write %s: %v", key, err)
	}
	return result, nil
}
func (s *Service) invalidateReports(ctx context.Context, uid int64) {
	if err := s.cache.DeleteByPrefix(ctx, reportPrefix(uid)); err != nil {
		log.Printf("ledger: invalidate reports: %v", err)
	}
}

type noCache struct{}

func (noCache) GetJSON(context.Context, string, any) (bool, error)        { return false, nil }
func (noCache) SetJSON(context.Context, string, any, time.Duration) error { return nil }
func (noCache) Delete(context.Context, ...string) error                   { return nil }
func (noCache) DeleteByPrefix(context.Context, string) error              { return nil }
