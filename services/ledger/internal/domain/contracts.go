package domain

import (
	"context"
	"time"
)

type Repository interface {
	SetBudget(context.Context, int64, Budget) error
	GetBudget(context.Context, int64, string) (Budget, error)
	ListBudgets(context.Context, int64) ([]Budget, error)
	AddTransaction(context.Context, int64, Transaction) (Transaction, error)
	ListTransactions(context.Context, int64) ([]Transaction, error)
	SumByCategory(context.Context, int64, string) (int64, error)
	GetReportSummary(context.Context, int64, string, string) (ReportSummary, error)
}

type TransactionManager interface {
	WithCategory(context.Context, int64, string, func(Repository) error) error
}

type Cache interface {
	GetJSON(context.Context, string, any) (bool, error)
	SetJSON(context.Context, string, any, time.Duration) error
	Delete(context.Context, ...string) error
	DeleteByPrefix(context.Context, string) error
}
