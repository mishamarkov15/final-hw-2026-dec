package domain

import (
	"math"
	"strings"
	"testing"
)

func TestTransactionValidation(t *testing.T) {
	valid := Transaction{Amount: 0.01, Category: "еда", Date: "2024-02-29"}
	tests := []struct {
		name   string
		change func(*Transaction)
	}{
		{"zero", func(v *Transaction) { v.Amount = 0 }},
		{"negative", func(v *Transaction) { v.Amount = -1 }},
		{"nan", func(v *Transaction) { v.Amount = math.NaN() }},
		{"infinite", func(v *Transaction) { v.Amount = math.Inf(1) }},
		{"too large", func(v *Transaction) { v.Amount = MaxMoney + 1 }},
		{"fractional cent", func(v *Transaction) { v.Amount = 1.001 }},
		{"empty category", func(v *Transaction) { v.Category = " \t" }},
		{"long category", func(v *Transaction) { v.Category = strings.Repeat("я", 101) }},
		{"long description", func(v *Transaction) { v.Description = strings.Repeat("я", 2001) }},
		{"invalid date", func(v *Transaction) { v.Date = "2025-02-29" }},
		{"date format", func(v *Transaction) { v.Date = "01.01.2026" }},
	}
	if err := CheckValid(valid); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := valid
			tt.change(&v)
			if err := CheckValid(v); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	valid.Amount = MaxMoney
	valid.Category = strings.Repeat("я", 100)
	valid.Description = strings.Repeat("я", 2000)
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestBudgetAndPeriodValidation(t *testing.T) {
	for _, b := range []Budget{{"еда", 0}, {"еда", -1}, {"", 1}, {"еда", 1.123}, {"еда", math.Inf(-1)}} {
		if err := CheckValid(b); err == nil {
			t.Fatalf("accepted %+v", b)
		}
	}
	if err := CheckValid(Budget{"еда", 5000}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Period{{"2026-01-01", "2025-01-01"}, {"bad", "2025-01-01"}, {"2025-01-01", "2025-02-29"}} {
		if err := p.Validate(); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	if err := (Period{"2026-01-01", "2026-01-01"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestCents(t *testing.T) {
	if got := ToCents(0.1) + ToCents(0.2); got != 30 {
		t.Fatalf("got %d", got)
	}
	for _, n := range []float64{0.01, 0.29, 123.45, MaxMoney} {
		if got := FromCents(ToCents(n)); got != n {
			t.Fatalf("%.2f => %.2f", n, got)
		}
	}
}
