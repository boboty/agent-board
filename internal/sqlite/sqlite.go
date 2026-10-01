// Package sqlite provides the SQLite connection bootstrap and transaction
// boundary shared by every Board process.
//
// Derived from rhizome-mcp (https://github.com/Odrin/rhizome-mcp), via
// boboty/agent-board-rhizome-poc, licensed under Apache-2.0. See NOTICE.
// Modified: removed backup and FTS5 requirements; Read always runs in one
// snapshot transaction.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	driverName            = "sqlite"
	defaultMaxOpenConns   = 4
	defaultMaxIdleConns   = 4
	defaultAutoCheckpoint = 1000
)

var defaultRetryDelays = []time.Duration{25 * time.Millisecond, 75 * time.Millisecond, 200 * time.Millisecond}

// Sleeper waits between complete write-transaction attempts.
type Sleeper interface {
	Sleep(context.Context, time.Duration) error
}

// SleepFunc adapts a function to Sleeper.
type SleepFunc func(context.Context, time.Duration) error

// Sleep implements Sleeper.
func (f SleepFunc) Sleep(ctx context.Context, delay time.Duration) error { return f(ctx, delay) }

// RetryPolicy controls bounded lock-contention retries. Delays are copied.
// A nil policy in Options selects the production defaults.
type RetryPolicy struct {
	Delays  []time.Duration
	Sleeper Sleeper
}

// Options contains test and production injection points for Open.
type Options struct {
	RetryPolicy *RetryPolicy
}

// DB is a configured SQLite connection pool. Its parent directory must exist
// before Open is called; Open creates neither directories nor schema objects.
type DB struct {
	pool   *sql.DB
	retry  retryPolicy
	path   string
	mu     sync.Mutex
	closed bool
}

// Open opens path, configures every pooled connection, and verifies WAL mode.
// The path must be non-empty and have an existing parent directory.
func Open(ctx context.Context, path string, options Options) (*DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, configurationError(errors.New("empty database path"), "database path must not be empty")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, configurationError(err, "database path is invalid")
	}
	info, err := os.Stat(filepath.Dir(absPath))
	if err != nil {
		return nil, configurationError(err, "database parent directory must exist")
	}
	if !info.IsDir() {
		return nil, configurationError(errors.New("database parent is not a directory"), "database parent directory must exist")
	}

	retry, err := newRetryPolicy(options.RetryPolicy)
	if err != nil {
		return nil, configurationError(err, "SQLite retry policy is invalid")
	}

	pool, err := sql.Open(driverName, dataSourceName(absPath))
	if err != nil {
		return nil, TranslateError(err)
	}
	pool.SetMaxOpenConns(defaultMaxOpenConns)
	pool.SetMaxIdleConns(defaultMaxIdleConns)
	pool.SetConnMaxLifetime(0)
	pool.SetConnMaxIdleTime(0)

	db := &DB{pool: pool, retry: retry, path: absPath}
	if err := db.verify(ctx); err != nil {
		closeErr := pool.Close()
		return nil, TranslateError(errors.Join(err, closeErr))
	}
	return db, nil
}

// Path returns the absolute database path.
func (db *DB) Path() string { return db.path }

// dataSourceName applies the per-connection PRAGMAs to every pooled
// connection. busy_timeout makes BEGIN IMMEDIATE wait for a competing writer
// in another process instead of failing at once.
func dataSourceName(databasePath string) string {
	uriPath := filepath.ToSlash(databasePath)
	if len(uriPath) >= 2 && uriPath[1] == ':' && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}

	query := url.Values{}
	for _, pragma := range []string{
		"foreign_keys(ON)",
		"synchronous(NORMAL)",
		"busy_timeout(5000)",
		"temp_store(MEMORY)",
		"trusted_schema(OFF)",
		"wal_autocheckpoint(1000)",
	} {
		query.Add("_pragma", pragma)
	}
	return (&url.URL{Scheme: "file", Path: uriPath, RawQuery: query.Encode()}).String()
}

func (db *DB) verify(ctx context.Context) error {
	if err := db.pool.PingContext(ctx); err != nil {
		return err
	}
	conn, err := db.pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	var mode string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode); err != nil {
		return err
	}
	if mode != "wal" {
		return configurationError(fmt.Errorf("journal mode %q", mode), "SQLite journal mode must be WAL")
	}
	var checkpoint int
	if err := conn.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&checkpoint); err != nil {
		return err
	}
	if checkpoint != defaultAutoCheckpoint {
		return configurationError(fmt.Errorf("WAL auto-checkpoint %d", checkpoint), "SQLite WAL auto-checkpoint is invalid")
	}
	return nil
}

// Close attempts a passive WAL checkpoint and always closes the pool.
func (db *DB) Close(ctx context.Context) error {
	if db == nil {
		return nil
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed || db.pool == nil {
		return nil
	}
	db.closed = true
	_, checkpointErr := db.pool.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)")
	closeErr := db.pool.Close()
	return TranslateError(errors.Join(checkpointErr, closeErr))
}
