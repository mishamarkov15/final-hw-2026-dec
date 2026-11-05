package transport

import (
	"context"
	"errors"
	"log"

	cashcraftv1 "github.com/mishamarkov15/final-hw-2026-dec/api/gen/v1"
	"github.com/mishamarkov15/final-hw-2026-dec/internal/authn"
	"github.com/mishamarkov15/final-hw-2026-dec/services/ledger/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Ledger interface {
	SetBudget(context.Context, int64, domain.Budget) (domain.Budget, error)
	ListBudgets(context.Context, int64) ([]domain.Budget, error)
	AddTransaction(context.Context, int64, domain.Transaction) (domain.Transaction, error)
	ListTransactions(context.Context, int64) ([]domain.Transaction, error)
	GetReportSummary(context.Context, int64, string, string) (domain.ReportSummary, error)
	ImportTransactions(context.Context, int64, string) (domain.ImportSummary, error)
	BulkTransactions(context.Context, int64, []domain.Transaction) (domain.ImportSummary, error)
	ExportTransactions(context.Context, int64) ([]byte, error)
}

type Server struct {
	cashcraftv1.UnimplementedLedgerServiceServer
	ledger Ledger
}

func New(ledger Ledger) *Server { return &Server{ledger: ledger} }

func (s *Server) SetBudget(ctx context.Context, in *cashcraftv1.Budget) (*cashcraftv1.Budget, error) {
	uid, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.ledger.SetBudget(ctx, uid, domain.Budget{Category: in.GetCategory(), Limit: in.GetLimit()})
	if err != nil {
		return nil, rpcError(err)
	}
	return budgetProto(result), nil
}
func (s *Server) ListBudgets(ctx context.Context, _ *cashcraftv1.Empty) (*cashcraftv1.BudgetsResponse, error) {
	uid, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.ledger.ListBudgets(ctx, uid)
	if err != nil {
		return nil, rpcError(err)
	}
	result := &cashcraftv1.BudgetsResponse{Budgets: make([]*cashcraftv1.Budget, 0, len(items))}
	for _, item := range items {
		result.Budgets = append(result.Budgets, budgetProto(item))
	}
	return result, nil
}
func (s *Server) AddTransaction(ctx context.Context, in *cashcraftv1.Transaction) (*cashcraftv1.Transaction, error) {
	uid, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.ledger.AddTransaction(ctx, uid, transactionDomain(in))
	if err != nil {
		return nil, rpcError(err)
	}
	return transactionProto(result), nil
}
func (s *Server) ListTransactions(ctx context.Context, _ *cashcraftv1.Empty) (*cashcraftv1.TransactionsResponse, error) {
	uid, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.ledger.ListTransactions(ctx, uid)
	if err != nil {
		return nil, rpcError(err)
	}
	result := &cashcraftv1.TransactionsResponse{Transactions: make([]*cashcraftv1.Transaction, 0, len(items))}
	for _, item := range items {
		result.Transactions = append(result.Transactions, transactionProto(item))
	}
	return result, nil
}
func (s *Server) GetReportSummary(ctx context.Context, in *cashcraftv1.ReportRequest) (*cashcraftv1.ReportResponse, error) {
	uid, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	report, err := s.ledger.GetReportSummary(ctx, uid, in.GetFrom(), in.GetTo())
	if err != nil {
		return nil, rpcError(err)
	}
	result := &cashcraftv1.ReportResponse{From: report.From, To: report.To, Total: report.Total, Categories: make([]*cashcraftv1.CategoryTotal, 0, len(report.Categories))}
	for _, item := range report.Categories {
		result.Categories = append(result.Categories, &cashcraftv1.CategoryTotal{Category: item.Category, Total: item.Total, HasBudget: item.HasBudget, BudgetLimit: item.BudgetLimit, BudgetUsedPercent: item.BudgetUsedPercent})
	}
	return result, nil
}
func (s *Server) ImportTransactions(ctx context.Context, in *cashcraftv1.ImportRequest) (*cashcraftv1.ImportResponse, error) {
	uid, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.ledger.ImportTransactions(ctx, uid, in.GetCsv())
	if err != nil {
		return nil, rpcError(err)
	}
	return importProto(result), nil
}
func (s *Server) BulkTransactions(ctx context.Context, in *cashcraftv1.BulkRequest) (*cashcraftv1.ImportResponse, error) {
	uid, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Transaction, 0, len(in.GetTransactions()))
	for _, item := range in.GetTransactions() {
		items = append(items, transactionDomain(item))
	}
	result, err := s.ledger.BulkTransactions(ctx, uid, items)
	if err != nil {
		return nil, rpcError(err)
	}
	return importProto(result), nil
}
func (s *Server) ExportTransactions(ctx context.Context, _ *cashcraftv1.Empty) (*cashcraftv1.ExportResponse, error) {
	uid, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.ledger.ExportTransactions(ctx, uid)
	if err != nil {
		return nil, rpcError(err)
	}
	return &cashcraftv1.ExportResponse{Data: result}, nil
}
func budgetProto(item domain.Budget) *cashcraftv1.Budget {
	return &cashcraftv1.Budget{Category: item.Category, Limit: item.Limit}
}
func transactionDomain(item *cashcraftv1.Transaction) domain.Transaction {
	return domain.Transaction{Amount: item.GetAmount(), Category: item.GetCategory(), Description: item.GetDescription(), Date: item.GetDate()}
}
func transactionProto(item domain.Transaction) *cashcraftv1.Transaction {
	return &cashcraftv1.Transaction{Id: item.ID, Amount: item.Amount, Category: item.Category, Description: item.Description, Date: item.Date}
}
func importProto(item domain.ImportSummary) *cashcraftv1.ImportResponse {
	result := &cashcraftv1.ImportResponse{Total: int32(item.Total), Created: int32(item.Created), Errors: make([]*cashcraftv1.ImportError, 0, len(item.Errors))}
	for _, err := range item.Errors {
		result.Errors = append(result.Errors, &cashcraftv1.ImportError{Row: int32(err.Row), Message: err.Message})
	}
	return result
}
func userID(ctx context.Context) (int64, error) {
	uid, err := authn.UserID(ctx)
	if err != nil {
		return 0, status.Error(codes.Unauthenticated, "authentication required")
	}
	return uid, nil
}
func rpcError(err error) error {
	var validation *domain.ValidationError
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	case errors.As(err, &validation):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrBudgetExceeded):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		log.Printf("ledger RPC failed: %v", err)
		return status.Error(codes.Internal, "internal server error")
	}
}
