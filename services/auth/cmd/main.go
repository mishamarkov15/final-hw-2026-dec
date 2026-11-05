package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/mishamarkov15/final-hw-2026-dec/api/gen/v1"
	"github.com/mishamarkov15/final-hw-2026-dec/internal/authn"
	"github.com/mishamarkov15/final-hw-2026-dec/internal/postgres"
	"github.com/mishamarkov15/final-hw-2026-dec/services/auth/internal/repository"
	"github.com/mishamarkov15/final-hw-2026-dec/services/auth/internal/service"
	"github.com/mishamarkov15/final-hw-2026-dec/services/auth/internal/transport"
	"google.golang.org/grpc"
)

func main() {
	if err := run(); err != nil {
		slog.Error("auth stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	db, err := postgres.Open(startup)
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()
	tokens, err := authn.FromEnv()
	if err != nil {
		return err
	}
	svc, err := service.New(repository.NewPostgres(db), tokens)
	if err != nil {
		return err
	}
	addr := os.Getenv("GRPC_ADDR")
	if addr == "" {
		addr = ":50052"
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer lis.Close()
	server := grpc.NewServer(grpc.MaxRecvMsgSize(64*1024), grpc.MaxConcurrentStreams(64))
	pb.RegisterAuthServiceServer(server, transport.New(svc))
	go func() {
		<-ctx.Done()
		done := make(chan struct{})
		go func() { server.GracefulStop(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			server.Stop()
		}
	}()
	slog.Info("auth listening", "address", addr)
	return server.Serve(lis)
}
