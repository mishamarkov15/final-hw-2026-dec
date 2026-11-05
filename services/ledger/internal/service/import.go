package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/mishamarkov15/final-hw-2026-dec/services/ledger/internal/domain"
)

const MaxImportRows = 1000
const importWorkers = 4

var csvHeader = []string{"amount", "category", "description", "date"}

type importItem struct {
	row int
	tx  domain.Transaction
	err error
}

func (s *Service) BulkTransactions(ctx context.Context, uid int64, records []domain.Transaction) (domain.ImportSummary, error) {
	if len(records) > MaxImportRows {
		return domain.ImportSummary{}, domain.Invalid("at most %d transactions per request", MaxImportRows)
	}
	jobs := make([]importItem, len(records))
	for i, item := range records {
		jobs[i] = importItem{row: i + 1, tx: item}
	}
	return s.importItems(ctx, uid, jobs)
}

func (s *Service) ImportTransactions(ctx context.Context, uid int64, data string) (domain.ImportSummary, error) {
	if err := ctx.Err(); err != nil {
		return domain.ImportSummary{}, err
	}
	reader := csv.NewReader(strings.NewReader(data))
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return domain.ImportSummary{}, domain.Invalid("read CSV header: %v", err)
	}
	if len(header) != len(csvHeader) {
		return domain.ImportSummary{}, domain.Invalid("CSV header must be amount,category,description,date")
	}
	for i, name := range csvHeader {
		if header[i] != name {
			return domain.ImportSummary{}, domain.Invalid("CSV header must be amount,category,description,date")
		}
	}
	// Parse the complete document before persisting any rows, so malformed CSV
	// cannot leave an unexpected partial import. Valid rows use best-effort import.
	var jobs []importItem
	for {
		if err := ctx.Err(); err != nil {
			return domain.ImportSummary{}, err
		}
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return domain.ImportSummary{}, domain.Invalid("parse CSV: %v", err)
		}
		if len(jobs) == MaxImportRows {
			return domain.ImportSummary{}, domain.Invalid("at most %d transactions per request", MaxImportRows)
		}
		line, _ := reader.FieldPos(0)
		job := importItem{row: line}
		if len(record) != 4 {
			job.err = domain.Invalid("expected 4 fields, got %d", len(record))
		} else {
			amount, err := strconv.ParseFloat(record[0], 64)
			if err != nil {
				job.err = domain.Invalid("amount must be a number")
			}
			job.tx = domain.Transaction{Amount: amount, Category: record[1], Description: record[2], Date: record[3]}
		}
		jobs = append(jobs, job)
	}
	return s.importItems(ctx, uid, jobs)
}

func (s *Service) importItems(ctx context.Context, uid int64, items []importItem) (domain.ImportSummary, error) {
	summary := domain.ImportSummary{Total: len(items), Errors: make([]domain.ImportError, 0)}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	jobs := make(chan importItem)
	results := make(chan domain.ImportError, importWorkers)
	var workers sync.WaitGroup
	for i := 0; i < importWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				err := item.err
				if err == nil {
					_, err = s.AddTransaction(ctx, uid, item.tx)
				}
				result := domain.ImportError{Row: item.row}
				if err != nil {
					result.Message = rowError(err)
				}
				results <- result
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, item := range items {
			select {
			case <-ctx.Done():
				return
			case jobs <- item:
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()
	for result := range results {
		if result.Message == "" {
			summary.Created++
		} else {
			summary.Errors = append(summary.Errors, result)
		}
	}
	sort.Slice(summary.Errors, func(i, j int) bool { return summary.Errors[i].Row < summary.Errors[j].Row })
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	return summary, nil
}
func rowError(err error) string {
	var invalid *domain.ValidationError
	switch {
	case errors.As(err, &invalid), errors.Is(err, domain.ErrBudgetExceeded):
		return err.Error()
	case errors.Is(err, context.Canceled):
		return "request canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "request deadline exceeded"
	default:
		log.Printf("ledger: import row: %v", err)
		return "could not save transaction"
	}
}

func (s *Service) ExportTransactions(ctx context.Context, uid int64) ([]byte, error) {
	records, err := s.ListTransactions(ctx, uid)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if err := writer.Write(csvHeader); err != nil {
		return nil, err
	}
	for _, item := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := writer.Write([]string{fmt.Sprintf("%.2f", item.Amount), item.Category, item.Description, item.Date}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
