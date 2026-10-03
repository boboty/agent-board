// Package migrations owns the embedded SQLite schema migration catalog and runner.
//
// The runner is derived from rhizome-mcp (https://github.com/Odrin/rhizome-mcp),
// via boboty/agent-board-rhizome-poc, licensed under Apache-2.0. See NOTICE.
// The schema itself is Agent Board's own.
package migrations

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/boboty/agent-board/internal/clock"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/sqlite"
)

//go:embed 001_task_store.sql
var taskStoreSQL string

//go:embed 002_fact_provenance.sql
var factProvenanceSQL string

// Tests verify each checksum against the exact embedded SQL bytes, so an
// applied migration cannot be edited silently. After an intentional edit to an
// unreleased migration, regenerate with: shasum -a 256 migrations/<file>.sql
const taskStoreChecksum = "ecb944e2435bab2898ba1ddf3f5237e797e3083ed39603c2d09e47bafa66a248"
const factProvenanceChecksum = "19f4131425448287df516ded1b833cb3c9a00906cd8a26d5cacd216ee936e681"

var (
	migrationNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)+$`)
	embeddedCatalog      = []migration{
		{version: 1, name: "task_store", checksum: taskStoreChecksum, sql: taskStoreSQL},
		{version: 2, name: "fact_provenance", checksum: factProvenanceChecksum, sql: factProvenanceSQL},
	}
)

// Result summarizes the database schema after a successful migration run.
type Result struct {
	Version int
	Applied int
}

type migration struct {
	version  int
	name     string
	checksum string
	sql      string
}

type historyRow struct {
	version   int
	name      string
	checksum  string
	appliedAt string
}

// CurrentVersion returns the highest migration version embedded in this binary.
func CurrentVersion() int {
	return embeddedCatalog[len(embeddedCatalog)-1].version
}

// Migrate validates the embedded catalog, takes SQLite's write lock up front,
// validates migration history, applies all pending scripts and their history
// rows atomically, and checks referential integrity. Concurrent runners in
// other processes serialize on the write lock; exactly one applies each
// migration. It never downgrades a database.
func Migrate(ctx context.Context, db *sqlite.DB, clk clock.Clock) (Result, error) {
	return run(ctx, db, clk, embeddedCatalog)
}

func run(ctx context.Context, db *sqlite.DB, clk clock.Clock, catalog []migration) (Result, error) {
	if err := validateCatalog(catalog); err != nil {
		return Result{}, err
	}
	if db == nil {
		return Result{}, migrationError(errors.New("nil SQLite database"), "migration database is required")
	}
	if clk == nil {
		return Result{}, migrationError(errors.New("nil migration clock"), "migration clock is required")
	}

	var result Result
	err := db.Write(ctx, func(ctx context.Context, tx sqlite.Executor) error {
		result = Result{}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at TEXT NOT NULL
		) STRICT`); err != nil {
			return migrationError(err, "cannot bootstrap migration history")
		}

		history, err := readHistory(ctx, tx)
		if err != nil {
			return err
		}
		if err := validateHistory(history, catalog); err != nil {
			return err
		}

		for _, item := range catalog[len(history):] {
			if _, err := tx.ExecContext(ctx, item.sql); err != nil {
				return migrationError(err, fmt.Sprintf("migration %d (%s) failed", item.version, item.name))
			}
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES (?, ?, ?, ?)",
				item.version, item.name, item.checksum, sqlite.FormatTime(clk.Now()),
			); err != nil {
				return migrationError(err, fmt.Sprintf("cannot record migration %d (%s)", item.version, item.name))
			}
			result.Applied++
		}
		result.Version = catalog[len(catalog)-1].version
		return checkForeignKeys(ctx, tx)
	})
	if err != nil {
		return Result{}, normalizeRunError(err)
	}
	return result, nil
}

func validateCatalog(catalog []migration) error {
	if len(catalog) == 0 {
		return migrationError(errors.New("empty migration catalog"), "embedded migration catalog is invalid")
	}
	seenNames := make(map[string]struct{}, len(catalog))
	for index, item := range catalog {
		if item.version != index+1 {
			return migrationError(
				fmt.Errorf("migration at index %d has version %d, expected %d", index, item.version, index+1),
				"embedded migration catalog has invalid version ordering",
			)
		}
		if !migrationNamePattern.MatchString(item.name) {
			return migrationError(fmt.Errorf("invalid migration name %q", item.name), "embedded migration catalog has invalid metadata")
		}
		if _, exists := seenNames[item.name]; exists {
			return migrationError(fmt.Errorf("duplicate migration name %q", item.name), "embedded migration catalog has duplicate names")
		}
		seenNames[item.name] = struct{}{}
		if len(item.sql) == 0 {
			return migrationError(fmt.Errorf("migration %d has empty SQL", item.version), "embedded migration catalog has invalid metadata")
		}
		actual := sha256.Sum256([]byte(item.sql))
		if item.checksum != hex.EncodeToString(actual[:]) {
			return migrationError(fmt.Errorf("migration %d checksum does not match SQL", item.version), "embedded migration catalog checksum is invalid")
		}
	}
	return nil
}

func readHistory(ctx context.Context, tx sqlite.Queryer) ([]historyRow, error) {
	rows, err := tx.QueryContext(ctx, "SELECT version, name, checksum, applied_at FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, migrationError(err, "cannot read migration history")
	}
	defer rows.Close()

	var history []historyRow
	for rows.Next() {
		var row historyRow
		if err := rows.Scan(&row.version, &row.name, &row.checksum, &row.appliedAt); err != nil {
			return nil, migrationError(err, "migration history is malformed")
		}
		history = append(history, row)
	}
	if err := rows.Err(); err != nil {
		return nil, migrationError(err, "cannot read migration history")
	}
	return history, nil
}

func validateHistory(history []historyRow, catalog []migration) error {
	if len(history) > len(catalog) {
		return migrationError(errors.New("database migration version is newer than this binary"), "database schema is newer than this application")
	}
	for index, row := range history {
		_, timestampErr := sqlite.ParseTime(row.appliedAt)
		if row.version != index+1 || strings.TrimSpace(row.name) == "" || timestampErr != nil {
			return migrationError(fmt.Errorf("malformed migration history row at index %d", index), "migration history is malformed")
		}
		item := catalog[index]
		if row.name != item.name {
			return migrationError(fmt.Errorf("migration %d name mismatch", row.version), "migration history name does not match the embedded catalog")
		}
		if row.checksum != item.checksum {
			return migrationError(fmt.Errorf("migration %d checksum mismatch", row.version), "migration history checksum does not match the embedded catalog")
		}
	}
	return nil
}

func checkForeignKeys(ctx context.Context, tx sqlite.Executor) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return migrationError(err, "cannot validate database foreign keys")
	}
	defer rows.Close()
	violations := 0
	for rows.Next() {
		violations++
	}
	if err := rows.Err(); err != nil {
		return migrationError(err, "cannot validate database foreign keys")
	}
	if violations > 0 {
		return domain.NewError(domain.CodeStorageMigration, fmt.Sprintf("database contains %d foreign key violations", violations), false)
	}
	return nil
}

func migrationError(cause error, message string) error {
	return domain.WrapError(cause, domain.CodeStorageMigration, message, false)
}

func normalizeRunError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if domain.IsCode(err, domain.CodeStorageMigration) || domain.IsCode(err, domain.CodeStorageBusy) {
		return err
	}
	return migrationError(err, "database migration failed")
}
