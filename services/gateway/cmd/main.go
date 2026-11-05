package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/mishamarkov15/final-hw-2026-dec/api/gen/v1"
	"github.com/mishamarkov15/final-hw-2026-dec/internal/authn"
	"github.com/mishamarkov15/final-hw-2026-dec/services/gateway/internal/httpapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	if err := run(); err != nil {
		slog.Error("gateway stopped", "error", err)
		os.Exit(1)
	}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tokens, err := authn.FromEnv()
	if err != nil {
		return err
	}
	auth, err := grpc.NewClient(env("AUTH_ADDR", "auth:50052"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer auth.Close()
	ledger, err := grpc.NewClient(env("LEDGER_ADDR", "ledger:50051"), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(12<<20), grpc.MaxCallRecvMsgSize(12<<20)))
	if err != nil {
		return err
	}
	defer ledger.Close()
	server := &http.Server{
		Addr:              env("HTTP_ADDR", ":8080"),
		Handler:           httpapi.New(pb.NewAuthServiceClient(auth), pb.NewLedgerServiceClient(ledger), tokens, env("OPENAPI_PATH", "docs/openapi.yaml")),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      25 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}()
	slog.Info("gateway listening", "address", server.Addr)
	err = server.ListenAndServe()
	stop()
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
