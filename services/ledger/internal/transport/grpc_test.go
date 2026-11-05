package transport

import (
	"context"
	"errors"
	"fmt"
	"testing"

	cashcraftv1 "github.com/mishamarkov15/final-hw-2026-dec/api/gen/v1"
	"github.com/mishamarkov15/final-hw-2026-dec/services/ledger/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRPCErrorMapping(t *testing.T) {
	tests := []struct {
		err  error
		code codes.Code
	}{
		{domain.Invalid("invalid amount"), codes.InvalidArgument},
		{fmt.Errorf("wrapped: %w", domain.ErrBudgetExceeded), codes.FailedPrecondition},
		{fmt.Errorf("wrapped: %w", context.Canceled), codes.Canceled},
		{fmt.Errorf("wrapped: %w", context.DeadlineExceeded), codes.DeadlineExceeded},
		{errors.New("postgres password=secret query"), codes.Internal},
	}
	for _, test := range tests {
		err := rpcError(test.err)
		if status.Code(err) != test.code {
			t.Fatalf("%v => %v", test.err, err)
		}
		if test.code == codes.Internal && status.Convert(err).Message() != "internal server error" {
			t.Fatal("internal details leaked")
		}
	}
}
func TestUnauthenticatedRequest(t *testing.T) {
	server := New(nil)
	if _, err := server.ListBudgets(context.Background(), &cashcraftv1.Empty{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("got %v", err)
	}
}
