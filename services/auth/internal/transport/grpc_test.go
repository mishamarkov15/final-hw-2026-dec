package transport

import (
	"context"
	"errors"
	"testing"

	"github.com/mishamarkov15/final-hw-2026-dec/services/auth/internal/domain"
	"github.com/mishamarkov15/final-hw-2026-dec/services/auth/internal/service"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAuthErrorCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code codes.Code
	}{
		{domain.ErrInvalidInput, codes.InvalidArgument},
		{domain.ErrInvalidCredentials, codes.Unauthenticated},
		{domain.ErrEmailExists, codes.AlreadyExists},
		{domain.ErrBusy, codes.ResourceExhausted},
		{context.DeadlineExceeded, codes.DeadlineExceeded},
		{context.Canceled, codes.Canceled},
		{errors.New("postgres password=secret"), codes.Internal},
	} {
		_, err := response(service.Result{}, tc.err)
		require.Equal(t, tc.code, status.Code(err))
		require.NotContains(t, err.Error(), "secret")
	}
}
