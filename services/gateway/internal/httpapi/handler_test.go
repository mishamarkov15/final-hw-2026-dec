package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	pb "github.com/mishamarkov15/final-hw-2026-dec/api/gen/v1"
	"github.com/mishamarkov15/final-hw-2026-dec/internal/authn"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const secret = "gateway-test-key-0123456789abcdef0123456789"

type ledgerStub struct {
	pb.LedgerServiceClient
	list         func(context.Context) (*pb.BudgetsResponse, error)
	transactions func(context.Context) (*pb.TransactionsResponse, error)
	report       func(context.Context, *pb.ReportRequest) (*pb.ReportResponse, error)
	importCSV    func(context.Context, *pb.ImportRequest) (*pb.ImportResponse, error)
	bulk         func(context.Context, *pb.BulkRequest) (*pb.ImportResponse, error)
}

func (s ledgerStub) ListBudgets(ctx context.Context, _ *pb.Empty, _ ...grpc.CallOption) (*pb.BudgetsResponse, error) {
	return s.list(ctx)
}
func (s ledgerStub) ListTransactions(ctx context.Context, _ *pb.Empty, _ ...grpc.CallOption) (*pb.TransactionsResponse, error) {
	return s.transactions(ctx)
}
func (s ledgerStub) GetReportSummary(ctx context.Context, req *pb.ReportRequest, _ ...grpc.CallOption) (*pb.ReportResponse, error) {
	return s.report(ctx, req)
}
func (s ledgerStub) ImportTransactions(ctx context.Context, req *pb.ImportRequest, _ ...grpc.CallOption) (*pb.ImportResponse, error) {
	return s.importCSV(ctx, req)
}
func (s ledgerStub) BulkTransactions(ctx context.Context, req *pb.BulkRequest, _ ...grpc.CallOption) (*pb.ImportResponse, error) {
	return s.bulk(ctx, req)
}

type authStub struct {
	pb.AuthServiceClient
	register func(context.Context, *pb.Credentials) (*pb.AuthResponse, error)
	login    func(context.Context, *pb.Credentials) (*pb.AuthResponse, error)
}

func (s authStub) Register(ctx context.Context, req *pb.Credentials, _ ...grpc.CallOption) (*pb.AuthResponse, error) {
	return s.register(ctx, req)
}

func (s authStub) Login(ctx context.Context, req *pb.Credentials, _ ...grpc.CallOption) (*pb.AuthResponse, error) {
	return s.login(ctx, req)
}

func fixture(t *testing.T, ledger pb.LedgerServiceClient) (*Handler, string) {
	t.Helper()
	m, err := authn.New(secret, time.Hour)
	require.NoError(t, err)
	token, _, err := m.Issue(42)
	require.NoError(t, err)
	return New(nil, ledger, m, ""), token
}

func request(h http.Handler, method, path, body, token, contentType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestProtectedRoutesRejectInvalidJWT(t *testing.T) {
	h, valid := fixture(t, nil)
	claims := jwt.RegisteredClaims{Issuer: "cashcraft", Subject: "42", Audience: jwt.ClaimStrings{"cashcraft-api"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute))}
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	for _, token := range []string{"", "garbage", expired, valid + "forged"} {
		for _, path := range []string{"/api/budgets", "/api/transactions", "/api/reports/summary", "/api/transactions/import", "/api/transactions/bulk", "/api/transactions/export.csv"} {
			w := request(h, "GET", path, "", token, "")
			require.Equal(t, http.StatusUnauthorized, w.Code)
			require.Equal(t, "Bearer", w.Header().Get("WWW-Authenticate"))
		}
	}
	r := httptest.NewRequest("GET", "/api/budgets", nil)
	r.Header.Add("Authorization", "Bearer "+valid)
	r.Header.Add("Authorization", "Bearer "+valid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, "pong", request(h, "GET", "/ping", "", "", "").Body.String())
}

func TestIdentityForwardingAndEmptyLists(t *testing.T) {
	var token string
	ledger := ledgerStub{list: func(ctx context.Context) (*pb.BudgetsResponse, error) {
		md, _ := metadata.FromOutgoingContext(ctx)
		require.Equal(t, []string{"Bearer " + token}, md.Get("authorization"))
		userID, err := authn.UserID(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(42), userID)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(15*time.Second), deadline, time.Second)
		return &pb.BudgetsResponse{}, nil
	}, transactions: func(context.Context) (*pb.TransactionsResponse, error) { return &pb.TransactionsResponse{}, nil }}
	h, issued := fixture(t, ledger)
	token = issued
	for _, path := range []string{"/api/budgets", "/api/transactions"} {
		w := request(h, "GET", path, "", token, "")
		require.Equal(t, http.StatusOK, w.Code)
		require.JSONEq(t, `[]`, w.Body.String())
	}
}

func TestRPCErrorMappingAndNoInternalDetails(t *testing.T) {
	for _, tc := range []struct {
		grpcCode codes.Code
		httpCode int
	}{
		{codes.InvalidArgument, 400}, {codes.FailedPrecondition, 409}, {codes.AlreadyExists, 409},
		{codes.Unauthenticated, 401}, {codes.PermissionDenied, 403}, {codes.NotFound, 404},
		{codes.Unavailable, 503}, {codes.DeadlineExceeded, 504}, {codes.ResourceExhausted, 429}, {codes.Internal, 500},
	} {
		t.Run(tc.grpcCode.String(), func(t *testing.T) {
			h, token := fixture(t, ledgerStub{list: func(context.Context) (*pb.BudgetsResponse, error) {
				return nil, status.Error(tc.grpcCode, "private SQL connection password")
			}})
			w := request(h, "GET", "/api/budgets", "", token, "")
			require.Equal(t, tc.httpCode, w.Code)
			if tc.grpcCode != codes.InvalidArgument && tc.grpcCode != codes.FailedPrecondition && tc.grpcCode != codes.AlreadyExists {
				require.NotContains(t, w.Body.String(), "password")
			}
		})
	}
	w := httptest.NewRecorder()
	rpcError(w, errors.New("private details"))
	require.Equal(t, 500, w.Code)
	require.NotContains(t, w.Body.String(), "private")
}

func TestStrictJSONAndBodyLimits(t *testing.T) {
	h, token := fixture(t, nil)
	for _, body := range []string{`{"category":"food","limit":100,"extra":true}`, `{"category":"food","limit":100} {}`, `{`, `{"limit":"100"}`} {
		w := request(h, "POST", "/api/budgets", body, token, "application/json")
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
	w := request(h, "POST", "/api/budgets", `{}`, token, "text/plain")
	require.Equal(t, http.StatusUnsupportedMediaType, w.Code)
	w = request(h, "POST", "/api/budgets", `{"category":"`+strings.Repeat("x", jsonBodyLimit)+`"}`, token, "application/json")
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	w = request(h, "POST", "/api/transactions/import", strings.Repeat("x", csvBodyLimit+1), token, "text/csv")
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	w = request(h, "DELETE", "/api/budgets", "", token, "")
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	require.Equal(t, "GET, POST", w.Header().Get("Allow"))
}

func TestRegisterDoesNotRequireToken(t *testing.T) {
	h, _ := fixture(t, nil)
	h.auth = authStub{register: func(ctx context.Context, in *pb.Credentials) (*pb.AuthResponse, error) {
		require.Equal(t, "test@example.com", in.GetEmail())
		require.Equal(t, "password123", in.GetPassword())
		return &pb.AuthResponse{AccessToken: "issued-token", ExpiresAt: 123, User: &pb.User{Id: 1, Email: in.GetEmail()}}, nil
	}}
	w := request(h, "POST", "/api/auth/register", `{"email":"test@example.com","password":"password123"}`, "", "application/json")
	require.Equal(t, http.StatusCreated, w.Code)
	require.JSONEq(t, `{"access_token":"issued-token","expires_at":123,"token_type":"Bearer","user":{"id":1,"email":"test@example.com"}}`, w.Body.String())
	require.NotContains(t, w.Body.String(), "password")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestLoginInvalidCredentialsAreGeneric(t *testing.T) {
	h, _ := fixture(t, nil)
	h.auth = authStub{login: func(context.Context, *pb.Credentials) (*pb.AuthResponse, error) {
		return nil, status.Error(codes.Unauthenticated, "untrusted diagnostic")
	}}
	w := request(h, "POST", "/api/auth/login", `{"email":"test@example.com","password":"wrong-password"}`, "", "application/json")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.JSONEq(t, `{"error":"invalid email or password"}`, w.Body.String())
}

func TestReportAndImportDTOs(t *testing.T) {
	ledger := ledgerStub{
		report: func(ctx context.Context, in *pb.ReportRequest) (*pb.ReportResponse, error) {
			require.Equal(t, "2026-01-01", in.GetFrom())
			require.Equal(t, "2026-01-31", in.GetTo())
			return &pb.ReportResponse{From: in.From, To: in.To, Total: 10, Categories: []*pb.CategoryTotal{{Category: "food", Total: 10}}}, nil
		},
		importCSV: func(ctx context.Context, in *pb.ImportRequest) (*pb.ImportResponse, error) {
			require.Equal(t, "amount,category,description,date\n10,food,lunch,2026-01-01\n", in.GetCsv())
			return &pb.ImportResponse{Total: 1, Created: 1}, nil
		},
		bulk: func(ctx context.Context, in *pb.BulkRequest) (*pb.ImportResponse, error) {
			require.Len(t, in.GetTransactions(), 1)
			require.Equal(t, "food", in.Transactions[0].Category)
			return &pb.ImportResponse{Total: 1, Errors: []*pb.ImportError{{Row: 1, Message: "budget exceeded"}}}, nil
		},
	}
	h, token := fixture(t, ledger)
	w := request(h, "GET", "/api/reports/summary?from=2026-01-01&to=2026-01-31", "", token, "")
	require.Equal(t, 200, w.Code)
	var report map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &report))
	category := report["categories"].([]any)[0].(map[string]any)
	require.Equal(t, false, category["has_budget"])
	require.Equal(t, float64(0), category["budget_limit"])
	require.Equal(t, float64(0), category["budget_used_percent"])
	w = request(h, "POST", "/api/transactions/import", "amount,category,description,date\n10,food,lunch,2026-01-01\n", token, "text/csv")
	require.Equal(t, 200, w.Code)
	require.JSONEq(t, `{"total":1,"created":1,"errors":[]}`, w.Body.String())
	w = request(h, "POST", "/api/transactions/bulk", `{"transactions":[{"amount":100,"category":"food","description":"lunch","date":"2026-01-01"}]}`, token, "application/json")
	require.Equal(t, 200, w.Code)
	require.JSONEq(t, `{"total":1,"created":0,"errors":[{"row":1,"message":"budget exceeded"}]}`, w.Body.String())
}
