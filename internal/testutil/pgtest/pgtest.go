//go:build integration

// Package pgtest starts a throwaway PostgreSQL for integration tests.
package pgtest

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jdolan-exalink/openvms/internal/platform/postgres"
	"github.com/jdolan-exalink/openvms/migrations"
)

// Start runs postgres:17-alpine and returns its URL (login role, a superuser).
func Start(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("openvms"),
		tcpostgres.WithUsername("openvms"),
		tcpostgres.WithPassword("openvms"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return url
}

// Migrated starts PostgreSQL, applies migrations and returns an application pool
// (role openvms_app, so row-level security applies).
func Migrated(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	url := Start(t)
	ctx := context.Background()
	if _, err := postgres.Migrate(ctx, url, migrations.FS, Discard()); err != nil {
		t.Fatal(err)
	}
	pool, err := postgres.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, url
}

func Discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
