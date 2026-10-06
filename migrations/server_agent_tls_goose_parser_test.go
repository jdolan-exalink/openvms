package migrations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/pressly/goose/v3"
)

var registerCaptureDriver sync.Once
var capturedMigrationSQL []string
var capturedMigrationMu sync.Mutex

type captureMigrationDriver struct{}
type captureMigrationConn struct{}
type captureMigrationTx struct{}

func (captureMigrationDriver) Open(string) (driver.Conn, error) { return captureMigrationConn{}, nil }
func (captureMigrationConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepare not supported")
}
func (captureMigrationConn) Close() error              { return nil }
func (captureMigrationConn) Begin() (driver.Tx, error) { return captureMigrationTx{}, nil }
func (captureMigrationConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return captureMigrationTx{}, nil
}
func (captureMigrationTx) Commit() error   { return nil }
func (captureMigrationTx) Rollback() error { return nil }
func (captureMigrationConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	capturedMigrationMu.Lock()
	defer capturedMigrationMu.Unlock()
	capturedMigrationSQL = append(capturedMigrationSQL, query)
	return driver.RowsAffected(1), nil
}
func (captureMigrationConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, fmt.Errorf("query not supported")
}

func TestServerAgentTLSRepairGooseKeepsDollarQuotedBlocksWhole(t *testing.T) {
	registerCaptureDriver.Do(func() { sql.Register("onvif_migration_capture", captureMigrationDriver{}) })
	goose.SetBaseFS(FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })

	migrations, err := goose.CollectMigrations(".", 32, 33)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 || migrations[0].Version != 33 {
		t.Fatalf("expected only migration 33, got %#v", migrations)
	}
	db, err := sql.Open("onvif_migration_capture", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, direction := range []string{"up", "down"} {
		t.Run(direction, func(t *testing.T) {
			capturedMigrationMu.Lock()
			capturedMigrationSQL = nil
			capturedMigrationMu.Unlock()
			var err error
			if direction == "up" {
				err = migrations[0].Up(db)
			} else {
				err = migrations[0].Down(db)
			}
			if err != nil {
				t.Fatalf("Goose %s parse/execute: %v", direction, err)
			}
			capturedMigrationMu.Lock()
			defer capturedMigrationMu.Unlock()
			if len(capturedMigrationSQL) != 2 {
				t.Fatalf("expected one dollar-quoted block plus version bookkeeping, got %d SQL statements: %#v", len(capturedMigrationSQL), capturedMigrationSQL)
			}
			block := capturedMigrationSQL[0]
			if !strings.HasPrefix(strings.TrimSpace(block), "DO $$") || !strings.Contains(block, "END $$;") || strings.Count(block, ";") < 2 {
				t.Fatalf("Goose split the %s dollar-quoted block: %q", direction, block)
			}
		})
	}
}
