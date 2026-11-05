package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxMoney = 999_999_999_999.99

var ErrNotFound = errors.New("not found")
var ErrBudgetExceeded = errors.New("budget exceeded")

type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }
func Invalid(format string, args ...any) error {
	return &ValidationError{Err: fmt.Errorf(format, args...)}
}

type Validatable interface{ Validate() error }

func CheckValid(v Validatable) error { return v.Validate() }

type Transaction struct {
	ID          int64   `json:"id"`
	Amount      float64 `json:"amount"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Date        string  `json:"date"`
}

func (t Transaction) Validate() error {
	if err := money(t.Amount, "amount"); err != nil {
		return err
	}
	if err := category(t.Category); err != nil {
		return err
	}
	if utf8.RuneCountInString(t.Description) > 2000 {
		return Invalid("description must not exceed 2000 characters")
	}
	if date, err := time.Parse("2006-01-02", t.Date); err != nil || date.Year() < 1 {
		return Invalid("date must be a real date in YYYY-MM-DD format")
	}
	return nil
}

type Budget struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
}

func (b Budget) Validate() error {
	if err := category(b.Category); err != nil {
		return err
	}
	return money(b.Limit, "limit")
}
func category(value string) error {
	if strings.TrimSpace(value) == "" {
		return Invalid("category must not be empty")
	}
	if utf8.RuneCountInString(value) > 100 {
		return Invalid("category must not exceed 100 characters")
	}
	return nil
}
func money(value float64, field string) error {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return Invalid("%s must be a positive finite number", field)
	}
	if value > MaxMoney {
		return Invalid("%s must not exceed %.2f", field, MaxMoney)
	}
	if math.Round(value*100)/100 != value {
		return Invalid("%s must have at most two decimal places", field)
	}
	return nil
}
func ToCents(value float64) int64   { return int64(math.Round(value * 100)) }
func FromCents(value int64) float64 { return float64(value) / 100 }

type Period struct{ From, To string }

func (p Period) Validate() error {
	from, err := time.Parse("2006-01-02", p.From)
	if err != nil || from.Year() < 1 {
		return Invalid("from must be a real date in YYYY-MM-DD format")
	}
	to, err := time.Parse("2006-01-02", p.To)
	if err != nil || to.Year() < 1 {
		return Invalid("to must be a real date in YYYY-MM-DD format")
	}
	if to.Before(from) {
		return Invalid("to must not be before from")
	}
	return nil
}

type CategoryTotal struct {
	Category          string  `json:"category"`
	Total             float64 `json:"total"`
	HasBudget         bool    `json:"has_budget"`
	BudgetLimit       float64 `json:"budget_limit"`
	BudgetUsedPercent float64 `json:"budget_used_percent"`
}
type ReportSummary struct {
	From       string          `json:"from"`
	To         string          `json:"to"`
	Total      float64         `json:"total"`
	Categories []CategoryTotal `json:"categories"`
}
type ImportError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}
type ImportSummary struct {
	Total   int           `json:"total"`
	Created int           `json:"created"`
	Errors  []ImportError `json:"errors"`
}
