package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/mishamarkov15/final-hw-2026-dec/services/ledger/internal/domain"
	"github.com/mishamarkov15/final-hw-2026-dec/services/ledger/internal/service"
)

func integrationStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { admin.Close() })
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "ledger_test_" + hex.EncodeToString(random[:])
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec("CREATE SCHEMA " + identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + identifier + " CASCADE"); err != nil {
			t.Error(err)
		}
	})
	cfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(10)
	t.Cleanup(func() { db.Close() })
	for _, name := range []string{"00001_users.sql", "00002_ledger.sql"} {
		contents, err := os.ReadFile("../../../../migrations/" + name)
		if errors.Is(err, os.ErrNotExist) && name == "00001_users.sql" {
			_, err = db.Exec(`CREATE TABLE users(id BIGSERIAL PRIMARY KEY,email TEXT UNIQUE NOT NULL,password_hash TEXT NOT NULL)`)
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		up := strings.Split(string(contents), "-- +goose Down")[0]
		if _, err := db.Exec(up); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO users(email,password_hash) VALUES ('one@example.com','test-hash'),('two@example.com','test-hash')`); err != nil {
		t.Fatal(err)
	}
	return New(db)
}
func tx(amount float64, category string) domain.Transaction {
	return domain.Transaction{Amount: amount, Category: category, Date: "2026-11-01"}
}

func TestPostgresIsolationCentsAndReport(t *testing.T) {
	repo := integrationStore(t)
	svc := service.New(repo, repo, nil)
	ctx := context.Background()
	if _, err := svc.SetBudget(ctx, 1, domain.Budget{Category: "еда", Limit: 0.3}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetBudget(ctx, 2, domain.Budget{Category: "еда", Limit: 20}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetBudget(ctx, 1, domain.Budget{Category: "досуг", Limit: 50}); err != nil {
		t.Fatal(err)
	}
	for _, amount := range []float64{0.1, 0.2} {
		if _, err := svc.AddTransaction(ctx, 1, tx(amount, "еда")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.AddTransaction(ctx, 1, tx(0.01, "еда")); !errors.Is(err, domain.ErrBudgetExceeded) {
		t.Fatalf("excess accepted: %v", err)
	}
	if _, err := svc.AddTransaction(ctx, 2, tx(15, "еда")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddTransaction(ctx, 1, tx(5, "такси")); err != nil {
		t.Fatal(err)
	}
	older := tx(10, "такси")
	older.Date = "2026-10-31"
	if _, err := svc.AddTransaction(ctx, 1, older); err != nil {
		t.Fatal(err)
	}
	report, err := svc.GetReportSummary(ctx, 1, "2026-11-01", "2026-11-01")
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 5.3 || len(report.Categories) != 3 {
		t.Fatalf("report: %+v", report)
	}
	for _, item := range report.Categories {
		switch item.Category {
		case "еда":
			if item.Total != 0.3 || !item.HasBudget || item.BudgetLimit != 0.3 || item.BudgetUsedPercent != 100 {
				t.Fatalf("food: %+v", item)
			}
		case "досуг":
			if item.Total != 0 || !item.HasBudget || item.BudgetUsedPercent != 0 {
				t.Fatalf("unused: %+v", item)
			}
		case "такси":
			if item.Total != 5 || item.HasBudget {
				t.Fatalf("no budget: %+v", item)
			}
		default:
			t.Fatalf("unexpected category: %+v", item)
		}
	}
	records, err := svc.ListTransactions(ctx, 2)
	if err != nil || len(records) != 1 || records[0].Amount != 15 {
		t.Fatalf("user isolation: %+v %v", records, err)
	}
	budgets, err := svc.ListBudgets(ctx, 2)
	if err != nil || len(budgets) != 1 || budgets[0].Limit != 20 {
		t.Fatalf("budget isolation: %+v %v", budgets, err)
	}
	if _, err := svc.SetBudget(ctx, 1, domain.Budget{Category: "еда", Limit: 0.1}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddTransaction(ctx, 1, tx(0.01, "еда")); !errors.Is(err, domain.ErrBudgetExceeded) {
		t.Fatalf("lowered budget ignored: %v", err)
	}
}
func TestPostgresConcurrentExpenses(t *testing.T) {
	repo := integrationStore(t)
	svc := service.New(repo, repo, nil)
	ctx := context.Background()
	if _, err := svc.SetBudget(ctx, 1, domain.Budget{Category: "еда", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := svc.AddTransaction(ctx, 1, tx(1, "еда")); results <- err }()
	}
	wg.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		} else if !errors.Is(err, domain.ErrBudgetExceeded) {
			t.Error(err)
		}
	}
	if created != 10 {
		t.Fatalf("created %d, expected exactly 10", created)
	}
	spent, err := repo.SumByCategory(ctx, 1, "еда")
	if err != nil || spent != 1000 {
		t.Fatalf("sum: %d %v", spent, err)
	}
}
func TestPostgresBudgetCreationSharesExpenseLock(t *testing.T) {
	repo := integrationStore(t)
	svc := service.New(repo, repo, nil)
	ctx := context.Background()
	locked := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- repo.WithCategory(ctx, 1, "new", func(r domain.Repository) error {
			if err := r.SetBudget(ctx, 1, domain.Budget{Category: "new", Limit: 1}); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-first:
		t.Fatalf("lock failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("lock timeout")
	}
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	second := make(chan error, 1)
	go func() { _, err := svc.AddTransaction(ctx, 1, tx(2, "new")); second <- err }()
	select {
	case err := <-second:
		t.Fatalf("expense bypassed lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; !errors.Is(err, domain.ErrBudgetExceeded) {
		t.Fatalf("expense ignored new budget: %v", err)
	}
}
func TestPostgresRollback(t *testing.T) {
	repo := integrationStore(t)
	ctx := context.Background()
	sentinel := errors.New("rollback")
	err := repo.WithCategory(ctx, 1, "еда", func(r domain.Repository) error {
		if _, err := r.AddTransaction(ctx, 1, tx(1, "еда")); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var count int
	if err := repo.db.QueryRow("SELECT COUNT(*) FROM expenses").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback failed: %d %v", count, err)
	}
	if _, err := repo.GetBudget(ctx, 1, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}
