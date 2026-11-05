package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/mishamarkov15/final-hw-2026-dec/internal/postgres"
	"github.com/pressly/goose/v3"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	database, err := postgres.Open(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatal(err)
	}
	directory := os.Getenv("MIGRATIONS_DIR")
	if directory == "" {
		directory = "migrations"
	}
	if err := goose.UpContext(ctx, database, directory); err != nil {
		log.Fatal(err)
	}
}
