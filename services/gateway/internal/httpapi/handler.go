package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	pb "github.com/mishamarkov15/final-hw-2026-dec/api/gen/v1"
	"github.com/mishamarkov15/final-hw-2026-dec/internal/authn"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	jsonBodyLimit = 2 << 20
	csvBodyLimit  = 10 << 20
)

type TokenVerifier interface{ Verify(string) (int64, error) }

type Handler struct {
	auth        pb.AuthServiceClient
	ledger      pb.LedgerServiceClient
	tokens      TokenVerifier
	openAPIPath string
}

func New(auth pb.AuthServiceClient, ledger pb.LedgerServiceClient, tokens TokenVerifier, openAPIPath string) *Handler {
	return &Handler{auth: auth, ledger: ledger, tokens: tokens, openAPIPath: openAPIPath}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	switch r.URL.Path {
	case "/ping":
		if !method(w, r, http.MethodGet) {
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "pong")
		return
	case "/docs":
		if !method(w, r, http.MethodGet) {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html lang="ru"><meta charset="utf-8"><title>Cashcraft API</title><h1>Cashcraft API</h1><p><a href="/openapi.yaml">OpenAPI specification</a></p></html>`)
		return
	case "/openapi.yaml":
		if !method(w, r, http.MethodGet) {
			return
		}
		data, err := os.ReadFile(h.openAPIPath)
		if err != nil {
			writeError(w, http.StatusNotFound, "API specification not found")
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(data)
		return
	case "/api/auth/register", "/api/auth/login":
		if !method(w, r, http.MethodPost) {
			return
		}
		h.credentials(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	if len(r.Header.Values("Authorization")) != 1 {
		unauthorized(w)
		return
	}
	token, err := authn.Bearer(r.Header.Get("Authorization"))
	if err != nil {
		unauthorized(w)
		return
	}
	userID, err := h.tokens.Verify(token)
	if err != nil {
		unauthorized(w)
		return
	}
	ctx = authn.WithUser(ctx, userID)
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	r = r.WithContext(ctx)
	switch r.URL.Path {
	case "/api/budgets":
		h.budgets(w, r)
	case "/api/transactions":
		h.transactions(w, r)
	case "/api/reports/summary":
		h.report(w, r)
	case "/api/transactions/import":
		h.importCSV(w, r)
	case "/api/transactions/bulk":
		h.bulk(w, r)
	case "/api/transactions/export.csv":
		h.exportCSV(w, r)
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, http.StatusUnauthorized, "valid bearer token required")
}

func method(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, jsonBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		bodyError(w, err)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		bodyError(w, err)
		return false
	}
	return true
}

func bodyError(w http.ResponseWriter, err error) {
	var max *http.MaxBytesError
	if errors.As(err, &max) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid request body")
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, struct {
		Error string `json:"error"`
	}{message})
}

func rpcError(w http.ResponseWriter, err error) {
	st := status.Convert(err)
	code := http.StatusInternalServerError
	message := "internal server error"
	switch st.Code() {
	case codes.InvalidArgument:
		code, message = http.StatusBadRequest, st.Message()
	case codes.FailedPrecondition, codes.AlreadyExists:
		code, message = http.StatusConflict, st.Message()
	case codes.Unauthenticated:
		unauthorized(w)
		return
	case codes.PermissionDenied:
		code, message = http.StatusForbidden, "permission denied"
	case codes.NotFound:
		code, message = http.StatusNotFound, "resource not found"
	case codes.Unavailable:
		code, message = http.StatusServiceUnavailable, "service unavailable"
	case codes.DeadlineExceeded:
		code, message = http.StatusGatewayTimeout, "service request timed out"
	case codes.ResourceExhausted:
		code, message = http.StatusTooManyRequests, "too many requests"
	case codes.Canceled:
		code, message = http.StatusRequestTimeout, "request canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code, message = http.StatusGatewayTimeout, "service request timed out"
	}
	if errors.Is(err, context.Canceled) {
		code, message = http.StatusRequestTimeout, "request canceled"
	}
	writeError(w, code, message)
}

func (h *Handler) credentials(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	credentials := &pb.Credentials{Email: input.Email, Password: input.Password}
	var response *pb.AuthResponse
	var err error
	code := http.StatusOK
	if r.URL.Path == "/api/auth/register" {
		response, err = h.auth.Register(r.Context(), credentials)
		code = http.StatusCreated
	} else {
		response, err = h.auth.Login(r.Context(), credentials)
	}
	if err != nil {
		if status.Code(err) == codes.Unauthenticated {
			writeError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		rpcError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, code, struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   int64  `json:"expires_at"`
		TokenType   string `json:"token_type"`
		User        struct {
			ID    int64  `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}{AccessToken: response.GetAccessToken(), ExpiresAt: response.GetExpiresAt(), TokenType: "Bearer", User: struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	}{response.GetUser().GetId(), response.GetUser().GetEmail()}})
}

type budgetDTO struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
}
type transactionDTO struct {
	ID          int64   `json:"id"`
	Amount      float64 `json:"amount"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Date        string  `json:"date"`
}
type transactionInput struct {
	Amount      float64 `json:"amount"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Date        string  `json:"date"`
}

func (t transactionInput) proto() *pb.Transaction {
	return &pb.Transaction{Amount: t.Amount, Category: t.Category, Description: t.Description, Date: t.Date}
}
func transaction(t *pb.Transaction) transactionDTO {
	return transactionDTO{t.GetId(), t.GetAmount(), t.GetCategory(), t.GetDescription(), t.GetDate()}
}

func (h *Handler) budgets(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	if r.Method == http.MethodPost {
		var input budgetDTO
		if !decodeJSON(w, r, &input) {
			return
		}
		response, err := h.ledger.SetBudget(r.Context(), &pb.Budget{Category: input.Category, Limit: input.Limit})
		if err != nil {
			rpcError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, budgetDTO{response.GetCategory(), response.GetLimit()})
		return
	}
	response, err := h.ledger.ListBudgets(r.Context(), &pb.Empty{})
	if err != nil {
		rpcError(w, err)
		return
	}
	result := make([]budgetDTO, 0, len(response.GetBudgets()))
	for _, b := range response.GetBudgets() {
		result = append(result, budgetDTO{b.GetCategory(), b.GetLimit()})
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) transactions(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	if r.Method == http.MethodPost {
		var input transactionInput
		if !decodeJSON(w, r, &input) {
			return
		}
		response, err := h.ledger.AddTransaction(r.Context(), input.proto())
		if err != nil {
			rpcError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, transaction(response))
		return
	}
	response, err := h.ledger.ListTransactions(r.Context(), &pb.Empty{})
	if err != nil {
		rpcError(w, err)
		return
	}
	result := make([]transactionDTO, 0, len(response.GetTransactions()))
	for _, t := range response.GetTransactions() {
		result = append(result, transaction(t))
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	query := r.URL.Query()
	response, err := h.ledger.GetReportSummary(r.Context(), &pb.ReportRequest{From: query.Get("from"), To: query.Get("to")})
	if err != nil {
		rpcError(w, err)
		return
	}
	type category struct {
		Category          string  `json:"category"`
		Total             float64 `json:"total"`
		HasBudget         bool    `json:"has_budget"`
		BudgetLimit       float64 `json:"budget_limit"`
		BudgetUsedPercent float64 `json:"budget_used_percent"`
	}
	result := struct {
		From       string     `json:"from"`
		To         string     `json:"to"`
		Total      float64    `json:"total"`
		Categories []category `json:"categories"`
	}{From: response.GetFrom(), To: response.GetTo(), Total: response.GetTotal(), Categories: make([]category, 0, len(response.GetCategories()))}
	for _, c := range response.GetCategories() {
		result.Categories = append(result.Categories, category{c.GetCategory(), c.GetTotal(), c.GetHasBudget(), c.GetBudgetLimit(), c.GetBudgetUsedPercent()})
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) importCSV(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "text/csv" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be text/csv")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, csvBodyLimit)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		bodyError(w, err)
		return
	}
	response, err := h.ledger.ImportTransactions(r.Context(), &pb.ImportRequest{Csv: string(data)})
	if err != nil {
		rpcError(w, err)
		return
	}
	writeImport(w, response)
}

func (h *Handler) bulk(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var input struct {
		Transactions []transactionInput `json:"transactions"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	transactions := make([]*pb.Transaction, 0, len(input.Transactions))
	for _, t := range input.Transactions {
		transactions = append(transactions, t.proto())
	}
	response, err := h.ledger.BulkTransactions(r.Context(), &pb.BulkRequest{Transactions: transactions})
	if err != nil {
		rpcError(w, err)
		return
	}
	writeImport(w, response)
}

func writeImport(w http.ResponseWriter, response *pb.ImportResponse) {
	type importError struct {
		Row     int32  `json:"row"`
		Message string `json:"message"`
	}
	result := struct {
		Total   int32         `json:"total"`
		Created int32         `json:"created"`
		Errors  []importError `json:"errors"`
	}{Total: response.GetTotal(), Created: response.GetCreated(), Errors: make([]importError, 0, len(response.GetErrors()))}
	for _, e := range response.GetErrors() {
		result.Errors = append(result.Errors, importError{e.GetRow(), e.GetMessage()})
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) exportCSV(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	response, err := h.ledger.ExportTransactions(r.Context(), &pb.Empty{})
	if err != nil {
		rpcError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="transactions.csv"`)
	_, _ = w.Write(response.GetData())
}
