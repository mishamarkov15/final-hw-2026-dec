package transport

import (
	"context"
	"errors"

	pb "github.com/mishamarkov15/final-hw-2026-dec/api/gen/v1"
	"github.com/mishamarkov15/final-hw-2026-dec/services/auth/internal/domain"
	"github.com/mishamarkov15/final-hw-2026-dec/services/auth/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedAuthServiceServer
	service *service.Service
}

func New(s *service.Service) *Server { return &Server{service: s} }

func (s *Server) Register(ctx context.Context, in *pb.Credentials) (*pb.AuthResponse, error) {
	r, err := s.service.Register(ctx, in.GetEmail(), in.GetPassword())
	return response(r, err)
}

func (s *Server) Login(ctx context.Context, in *pb.Credentials) (*pb.AuthResponse, error) {
	r, err := s.service.Login(ctx, in.GetEmail(), in.GetPassword())
	return response(r, err)
}

func response(r service.Result, err error) (*pb.AuthResponse, error) {
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidInput):
			return nil, status.Error(codes.InvalidArgument, err.Error())
		case errors.Is(err, domain.ErrInvalidCredentials):
			return nil, status.Error(codes.Unauthenticated, domain.ErrInvalidCredentials.Error())
		case errors.Is(err, domain.ErrEmailExists):
			return nil, status.Error(codes.AlreadyExists, domain.ErrEmailExists.Error())
		case errors.Is(err, domain.ErrBusy):
			return nil, status.Error(codes.ResourceExhausted, domain.ErrBusy.Error())
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return nil, status.FromContextError(err).Err()
		default:
			return nil, status.Error(codes.Internal, "authentication service failed")
		}
	}
	return &pb.AuthResponse{AccessToken: r.AccessToken, ExpiresAt: r.ExpiresAt.Unix(), User: &pb.User{Id: r.User.ID, Email: r.User.Email}}, nil
}
