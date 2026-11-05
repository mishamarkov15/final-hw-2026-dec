package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mishamarkov15/final-hw-2026-dec/services/ledger/internal/domain"
)

type memoryRepo struct {
	mu           sync.Mutex
	txMu         sync.Mutex
	budgets      map[int64]map[string]domain.Budget
	transactions map[int64][]domain.Transaction
	calls        int
}

func newRepo() *memoryRepo {
	return &memoryRepo{budgets: make(map[int64]map[string]domain.Budget), transactions: make(map[int64][]domain.Transaction)}
}
func (r *memoryRepo) WithCategory(ctx context.Context, uid int64, category string, fn func(domain.Repository) error) error {
	r.txMu.Lock()
	defer r.txMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(r)
}
func (r *memoryRepo) SetBudget(_ context.Context, uid int64, b domain.Budget) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.budgets[uid] == nil {
		r.budgets[uid] = make(map[string]domain.Budget)
	}
	r.budgets[uid][b.Category] = b
	return nil
}
func (r *memoryRepo) GetBudget(_ context.Context, uid int64, category string) (domain.Budget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.budgets[uid][category]
	if !ok {
		return b, domain.ErrNotFound
	}
	return b, nil
}
func (r *memoryRepo) ListBudgets(_ context.Context, uid int64) ([]domain.Budget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	result := make([]domain.Budget, 0)
	for _, b := range r.budgets[uid] {
		result = append(result, b)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Category < result[j].Category })
	return result, nil
}
func (r *memoryRepo) AddTransaction(ctx context.Context, uid int64, item domain.Transaction) (domain.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.Transaction{}, err
	}
	item.ID = int64(len(r.transactions[uid]) + 1)
	r.transactions[uid] = append(r.transactions[uid], item)
	return item, nil
}
func (r *memoryRepo) ListTransactions(_ context.Context, uid int64) ([]domain.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.Transaction{}, r.transactions[uid]...), nil
}
func (r *memoryRepo) SumByCategory(_ context.Context, uid int64, category string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total int64
	for _, item := range r.transactions[uid] {
		if item.Category == category {
			total += domain.ToCents(item.Amount)
		}
	}
	return total, nil
}
func (r *memoryRepo) GetReportSummary(_ context.Context, uid int64, from, to string) (domain.ReportSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	result := domain.ReportSummary{From: from, To: to, Categories: make([]domain.CategoryTotal, 0)}
	var total int64
	for _, item := range r.transactions[uid] {
		if item.Date >= from && item.Date <= to {
			total += domain.ToCents(item.Amount)
		}
	}
	result.Total = domain.FromCents(total)
	return result, nil
}

type memoryCache struct {
	mu     sync.Mutex
	values map[string][]byte
	ttl    map[string]time.Duration
	fail   bool
}

func newCache() *memoryCache {
	return &memoryCache{values: make(map[string][]byte), ttl: make(map[string]time.Duration)}
}
func (c *memoryCache) GetJSON(_ context.Context, key string, dst any) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return false, errors.New("offline")
	}
	raw, ok := c.values[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, dst)
}
func (c *memoryCache) SetJSON(_ context.Context, key string, value any, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return errors.New("offline")
	}
	raw, err := json.Marshal(value)
	c.values[key] = raw
	c.ttl[key] = ttl
	return err
}
func (c *memoryCache) Delete(_ context.Context, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		delete(c.values, key)
	}
	return nil
}
func (c *memoryCache) DeleteByPrefix(_ context.Context, prefix string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.values {
		if strings.HasPrefix(key, prefix) {
			delete(c.values, key)
		}
	}
	return nil
}
func transaction(amount float64) domain.Transaction {
	return domain.Transaction{Amount: amount, Category: "еда", Date: "2026-11-01"}
}

func TestBudgetCentsValidationAndUserIsolation(t *testing.T) {
	ctx := context.Background()
	repo := newRepo()
	svc := New(repo, repo, nil)
	if _, err := svc.SetBudget(ctx, 1, domain.Budget{Category: " еда ", Limit: 0.3}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddTransaction(ctx, 1, transaction(0.1)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddTransaction(ctx, 1, transaction(0.2)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddTransaction(ctx, 1, transaction(0.01)); !errors.Is(err, domain.ErrBudgetExceeded) {
		t.Fatalf("got %v", err)
	}
	if _, err := svc.AddTransaction(ctx, 2, transaction(100)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddTransaction(ctx, 1, transaction(-1)); err == nil {
		t.Fatal("negative amount accepted")
	}
	if _, err := svc.SetBudget(ctx, 1, domain.Budget{Category: "еда", Limit: 0}); err == nil {
		t.Fatal("zero budget accepted")
	}
	if _, err := svc.SetBudget(ctx, 1, domain.Budget{Category: "еда", Limit: 0.1}); err != nil {
		t.Fatal("lower budget should be allowed:", err)
	}
	one, _ := svc.ListTransactions(ctx, 1)
	two, _ := svc.ListTransactions(ctx, 2)
	if len(one) != 2 || len(two) != 1 {
		t.Fatalf("user data leaked: %v %v", one, two)
	}
}
func TestConcurrentBudgetLimit(t *testing.T) {
	ctx := context.Background()
	repo := newRepo()
	svc := New(repo, repo, nil)
	_, err := svc.SetBudget(ctx, 1, domain.Budget{Category: "еда", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := svc.AddTransaction(ctx, 1, transaction(1)); errs <- err }()
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else if !errors.Is(err, domain.ErrBudgetExceeded) {
			t.Fatal(err)
		}
	}
	if success != 10 {
		t.Fatalf("saved %d, expected 10", success)
	}
}
func TestCacheTTLInvalidationAndFallback(t *testing.T) {
	ctx := context.Background()
	repo := newRepo()
	cache := newCache()
	svc := New(repo, repo, cache)
	_, _ = svc.SetBudget(ctx, 1, domain.Budget{Category: "еда", Limit: 10})
	_, _ = svc.ListBudgets(ctx, 1)
	_, _ = svc.ListBudgets(ctx, 1)
	if repo.calls != 1 || cache.ttl["budgets:1:all"] != 30*time.Second {
		t.Fatal("budget cache missed or wrong TTL")
	}
	_, _ = svc.GetReportSummary(ctx, 1, "2026-11-01", "2026-11-30")
	_, _ = svc.GetReportSummary(ctx, 1, "2026-11-01", "2026-11-30")
	_, _ = svc.GetReportSummary(ctx, 2, "2026-11-01", "2026-11-30")
	if repo.calls != 3 {
		t.Fatalf("unexpected calls: %d", repo.calls)
	}
	key := "report:summary:1:2026-11-01:2026-11-30"
	if cache.ttl[key] != CacheTTL {
		t.Fatal("report TTL")
	}
	_, _ = svc.AddTransaction(ctx, 1, transaction(2))
	if _, ok := cache.values[key]; ok {
		t.Fatal("report not invalidated after expense")
	}
	if _, ok := cache.values["report:summary:2:2026-11-01:2026-11-30"]; !ok {
		t.Fatal("another user's report invalidated")
	}
	report, _ := svc.GetReportSummary(ctx, 1, "2026-11-01", "2026-11-30")
	if report.Total != 2 {
		t.Fatal(report)
	}
	_, _ = svc.SetBudget(ctx, 1, domain.Budget{Category: "еда", Limit: 20})
	if _, ok := cache.values[key]; ok {
		t.Fatal("report not invalidated after budget")
	}
	if _, ok := cache.values["budgets:1:all"]; ok {
		t.Fatal("budget cache not invalidated")
	}
	cache.fail = true
	got, err := svc.ListBudgets(ctx, 1)
	if err != nil || len(got) != 1 || got[0].Limit != 20 {
		t.Fatalf("fail-open budgets: %v %v", got, err)
	}
	report, err = svc.GetReportSummary(ctx, 1, "2026-11-01", "2026-11-30")
	if err != nil || report.Total != 2 {
		t.Fatalf("fail-open report: %v %v", report, err)
	}
}
func TestBulkAndCSV(t *testing.T) {
	ctx := context.Background()
	repo := newRepo()
	svc := New(repo, repo, nil)
	result, err := svc.BulkTransactions(ctx, 1, []domain.Transaction{transaction(1), transaction(-1), transaction(0), transaction(2)})
	if err != nil || result.Total != 4 || result.Created != 2 || len(result.Errors) != 2 || result.Errors[0].Row != 2 || result.Errors[1].Row != 3 {
		t.Fatalf("bulk: %+v %v", result, err)
	}
	data := "amount,category,description,date\n1,еда,\"two\nlines\",2026-11-01\nno,еда,bad,2026-11-01\n3,еда,missing\n"
	result, err = svc.ImportTransactions(ctx, 2, data)
	if err != nil || result.Total != 3 || result.Created != 1 || len(result.Errors) != 2 || result.Errors[0].Row != 4 || result.Errors[1].Row != 5 {
		t.Fatalf("CSV: %+v %v", result, err)
	}
	exported, err := svc.ExportTransactions(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	result, err = svc.ImportTransactions(ctx, 3, string(exported))
	if err != nil || result.Created != 1 {
		t.Fatalf("roundtrip: %+v %v", result, err)
	}
	a, _ := svc.ListTransactions(ctx, 2)
	b, _ := svc.ListTransactions(ctx, 3)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("roundtrip mismatch: %v %v", a, b)
	}
	for _, bad := range []string{"bad,header\n", "amount,category,description,date\n1,еда,valid,2026-11-01\n2,еда,\"broken,2026-11-01\n"} {
		if _, err := svc.ImportTransactions(ctx, 4, bad); err == nil {
			t.Fatal("accepted malformed CSV")
		}
	}
	records, _ := svc.ListTransactions(ctx, 4)
	if len(records) != 0 {
		t.Fatal("malformed CSV wrote rows")
	}
	if _, err := svc.BulkTransactions(ctx, 4, make([]domain.Transaction, MaxImportRows+1)); err == nil {
		t.Fatal("unbounded bulk")
	}
}
func TestCanceledImport(t *testing.T) {
	repo := newRepo()
	svc := New(repo, repo, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.BulkTransactions(ctx, 1, []domain.Transaction{transaction(1)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	records, _ := svc.ListTransactions(context.Background(), 1)
	if len(records) != 0 {
		t.Fatal("canceled request wrote rows")
	}
}

type blockingTransactions struct {
	entered      chan struct{}
	mu           sync.Mutex
	active, peak int
}

func (b *blockingTransactions) WithCategory(ctx context.Context, _ int64, _ string, _ func(domain.Repository) error) error {
	b.mu.Lock()
	b.active++
	if b.active > b.peak {
		b.peak = b.active
	}
	b.mu.Unlock()
	defer func() { b.mu.Lock(); b.active--; b.mu.Unlock() }()
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}
func TestBulkCancellationDrainsBoundedWorkers(t *testing.T) {
	repo := newRepo()
	manager := &blockingTransactions{entered: make(chan struct{}, 100)}
	svc := New(repo, manager, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	records := make([]domain.Transaction, 100)
	for i := range records {
		records[i] = transaction(1)
	}
	done := make(chan error, 1)
	go func() { _, err := svc.BulkTransactions(ctx, 1, records); done <- err }()
	for i := 0; i < importWorkers; i++ {
		select {
		case <-manager.entered:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled bulk did not stop")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.active != 0 || manager.peak != importWorkers {
		t.Fatalf("active=%d peak=%d", manager.active, manager.peak)
	}
}
