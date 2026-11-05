package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	cashcraftv1 "github.com/mishamarkov15/final-hw-2026-dec/api/gen/v1"
	"github.com/mishamarkov15/final-hw-2026-dec/internal/authn"
	"github.com/mishamarkov15/final-hw-2026-dec/internal/postgres"
	"github.com/mishamarkov15/final-hw-2026-dec/services/ledger/internal/cache"
	"github.com/mishamarkov15/final-hw-2026-dec/services/ledger/internal/repository"
	"github.com/mishamarkov15/final-hw-2026-dec/services/ledger/internal/service"
	"github.com/mishamarkov15/final-hw-2026-dec/services/ledger/internal/transport"
	"google.golang.org/grpc"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	manager, err := authn.FromEnv()
	if err != nil {
		return err
	}
	db, err := postgres.Open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	config, err := cache.ConfigFromEnv()
	if err != nil {
		return err
	}
	redis, err := cache.New(ctx, config)
	if err != nil {
		return fmt.Errorf("connect Redis: %w", err)
	}
	defer redis.Close()
	repo := repository.New(db)
	ledger := service.New(repo, repo, redis)
	address := os.Getenv("GRPC_ADDR")
	if address == "" {
		address = ":50051"
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := grpc.NewServer(grpc.UnaryInterceptor(authn.UnaryServerInterceptor(manager)), grpc.MaxRecvMsgSize(12<<20), grpc.MaxSendMsgSize(12<<20))
	cashcraftv1.RegisterLedgerServiceServer(server, transport.New(ledger))
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	log.Printf("Ledger gRPC listening on %s", address)
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		done := make(chan struct{})
		go func() { server.GracefulStop(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			server.Stop()
			<-done
		}
		return nil
	}
}
