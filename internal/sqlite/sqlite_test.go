package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/boboty/agent-board/internal/domain"

	moderncsqlite "modernc.org/sqlite"
)

func TestOpenConfiguresWALAndPragmas(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "board.db"), Options{})
	err := db.Read(context.Background(), func(ctx context.Context, q Queryer) error {
		for pragma, want := range map[string]string{"journal_mode": "wal", "foreign_keys": "1", "busy_timeout": "5000", "synchronous": "1"} {
			var got string
			if err := q.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
				return err
			}
			if got != want {
				t.Errorf("PRAGMA %s = %q, want %q", pragma, got, want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenRequiresExistingParent(t *testing.T) {
	_, err := Open(context.Background(), filepath.Join(t.TempDir(), "missing", "board.db"), Options{})
	if !domain.IsCode(err, domain.CodeStorageConfiguration) {
		t.Fatalf("Open() error = %v, want %s", err, domain.CodeStorageConfiguration)
	}
}

func TestWriteCommitsAndRollsBack(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "board.db"), noRetry())
	ctx := context.Background()
	mustWrite(t, db, "CREATE TABLE v (value TEXT NOT NULL)")
	mustWrite(t, db, "INSERT INTO v(value) VALUES ('committed')")

	reject := errors.New("reject")
	err := db.Write(ctx, func(ctx context.Context, tx Executor) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO v(value) VALUES ('rolled back')"); err != nil {
			return err
		}
		return reject
	})
	if !errors.Is(err, reject) {
		t.Fatalf("Write() error = %v, want callback error", err)
	}
	if got := count(t, db, "SELECT count(*) FROM v"); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
}

func TestWriteRetriesBusyThenMapsExhaustion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	delays := []time.Duration{time.Millisecond, 2 * time.Millisecond}
	sleeper := &recordingSleeper{}
	db := openTestDB(t, path, Options{RetryPolicy: &RetryPolicy{Delays: delays, Sleeper: sleeper}})
	busy := obtainBusyError(t, filepath.Join(t.TempDir(), "busy.db"))

	attempts := 0
	err := db.Write(context.Background(), func(context.Context, Executor) error {
		attempts++
		return busy
	})
	if !domain.IsCode(err, domain.CodeStorageBusy) {
		t.Fatalf("Write() error = %v, want STORAGE_BUSY", err)
	}
	if attempts != len(delays)+1 {
		t.Fatalf("attempts = %d, want %d", attempts, len(delays)+1)
	}
	if got := sleeper.Delays(); !reflect.DeepEqual(got, delays) {
		t.Fatalf("delays = %v, want %v", got, delays)
	}
	var sqliteErr *moderncsqlite.Error
	if !errors.As(err, &sqliteErr) {
		t.Fatal("translated busy error lost SQLite cause")
	}
}

func TestWriteDoesNotRetryDomainOrConstraintErrors(t *testing.T) {
	sleeper := &recordingSleeper{}
	db := openTestDB(t, filepath.Join(t.TempDir(), "board.db"), Options{RetryPolicy: &RetryPolicy{Delays: defaultRetryDelays, Sleeper: sleeper}})
	mustWrite(t, db, "CREATE TABLE u (value TEXT UNIQUE)")
	mustWrite(t, db, "INSERT INTO u VALUES ('same')")

	attempts := 0
	err := db.Write(context.Background(), func(ctx context.Context, tx Executor) error {
		attempts++
		_, err := tx.ExecContext(ctx, "INSERT INTO u VALUES ('same')")
		return err
	})
	if !domain.IsCode(err, domain.CodeStorageConstraint) || attempts != 1 {
		t.Fatalf("constraint: error = %v attempts = %d", err, attempts)
	}

	attempts = 0
	err = db.Write(context.Background(), func(context.Context, Executor) error {
		attempts++
		return domain.NewError(domain.CodeVersionConflict, "conflict", true)
	})
	if !domain.IsCode(err, domain.CodeVersionConflict) || attempts != 1 || len(sleeper.Delays()) != 0 {
		t.Fatalf("domain: error = %v attempts = %d delays = %v", err, attempts, sleeper.Delays())
	}
}

func TestWriteCancellationStopsDuringRetryWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sleeper := SleepFunc(func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	})
	db := openTestDB(t, filepath.Join(t.TempDir(), "board.db"), Options{RetryPolicy: &RetryPolicy{Delays: []time.Duration{time.Hour}, Sleeper: sleeper}})
	busy := obtainBusyError(t, filepath.Join(t.TempDir(), "busy.db"))
	err := db.Write(ctx, func(context.Context, Executor) error { return busy })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Write() error = %v, want context.Canceled", err)
	}
}

// TestDeferredUpgradeFailsButImmediateWriteDoesNot documents why Write uses
// BEGIN IMMEDIATE. Two deferred transactions that both read and then write
// deadlock on the lock upgrade, and SQLite fails one at once with BUSY
// without consulting busy_timeout. Read-check-write through Write from
// separate pools (separate processes, in effect) all succeed.
func TestDeferredUpgradeFailsButImmediateWriteDoesNot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	setup := openTestDB(t, path, Options{})
	mustWrite(t, setup, "CREATE TABLE counter (n INTEGER NOT NULL)")
	mustWrite(t, setup, "INSERT INTO counter VALUES (0)")

	ctx := context.Background()
	raw1, raw2 := rawConn(t, path), rawConn(t, path)
	for _, c := range []*sql.Conn{raw1, raw2} {
		if _, err := c.ExecContext(ctx, "BEGIN"); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := c.QueryRowContext(ctx, "SELECT n FROM counter").Scan(&n); err != nil {
			t.Fatal(err)
		}
	}
	_, err1 := raw1.ExecContext(ctx, "UPDATE counter SET n = n + 1")
	_, err2 := raw2.ExecContext(ctx, "UPDATE counter SET n = n + 1")
	if err1 == nil {
		if _, err := raw1.ExecContext(ctx, "COMMIT"); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = raw2.ExecContext(ctx, "ROLLBACK")
	if err1 == nil && err2 == nil || !isLockContention(errors.Join(err1, err2)) {
		t.Fatalf("deferred upgrade errors = %v / %v, want one SQLITE_BUSY", err1, err2)
	}

	const writers, increments = 6, 40
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for w := 0; w < writers; w++ {
		db := openTestDB(t, path, Options{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < increments; i++ {
				err := db.Write(ctx, func(ctx context.Context, tx Executor) error {
					var n int
					if err := tx.QueryRowContext(ctx, "SELECT n FROM counter").Scan(&n); err != nil {
						return err
					}
					_, err := tx.ExecContext(ctx, "UPDATE counter SET n = ?", n+1)
					return err
				})
				if err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Write() error = %v", err)
	}
	want := 1 + writers*increments
	if got := count(t, setup, "SELECT n FROM counter"); got != want {
		t.Fatalf("counter = %d, want %d (lost updates)", got, want)
	}
}

func TestReadSnapshotDoesNotBlockWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	db := openTestDB(t, path, noRetry())
	mustWrite(t, db, "CREATE TABLE v (n INTEGER)")
	mustWrite(t, db, "INSERT INTO v VALUES (1)")
	writer := openTestDB(t, path, noRetry())

	err := db.Read(context.Background(), func(ctx context.Context, q Queryer) error {
		if got := scanInt(t, q, "SELECT count(*) FROM v"); got != 1 {
			t.Fatalf("before = %d", got)
		}
		if err := writer.Write(ctx, func(ctx context.Context, tx Executor) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO v VALUES (2)")
			return err
		}); err != nil {
			t.Fatalf("writer blocked by reader: %v", err)
		}
		if got := scanInt(t, q, "SELECT count(*) FROM v"); got != 1 {
			t.Fatalf("snapshot changed mid-read: %d", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func openTestDB(t *testing.T, path string, options Options) *DB {
	t.Helper()
	db, err := Open(context.Background(), path, options)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	return db
}

func noRetry() Options {
	return Options{RetryPolicy: &RetryPolicy{Sleeper: SleepFunc(func(context.Context, time.Duration) error { return nil })}}
}

func mustWrite(t *testing.T, db *DB, statement string) {
	t.Helper()
	if err := db.Write(context.Background(), func(ctx context.Context, tx Executor) error {
		_, err := tx.ExecContext(ctx, statement)
		return err
	}); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

func count(t *testing.T, db *DB, query string) int {
	t.Helper()
	var n int
	if err := db.Read(context.Background(), func(ctx context.Context, q Queryer) error {
		n = scanInt(t, q, query)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func scanInt(t *testing.T, q Queryer, query string) int {
	t.Helper()
	var n int
	if err := q.QueryRowContext(context.Background(), query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func rawConn(t *testing.T, path string) *sql.Conn {
	t.Helper()
	pool, err := sql.Open(driverName, dataSourceName(path))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(); pool.Close() })
	return conn
}

type recordingSleeper struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (s *recordingSleeper) Sleep(ctx context.Context, delay time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delays = append(s.delays, delay)
	return ctx.Err()
}

func (s *recordingSleeper) Delays() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.delays...)
}

// obtainBusyError produces a genuine SQLITE_BUSY by contending for the write
// lock with busy_timeout disabled.
func obtainBusyError(t *testing.T, path string) error {
	t.Helper()
	ctx := context.Background()
	owner := rawConn(t, path)
	if _, err := owner.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer owner.ExecContext(ctx, "ROLLBACK")
	contender, err := sql.Open(driverName, "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatal(err)
	}
	defer contender.Close()
	_, err = contender.ExecContext(ctx, "BEGIN IMMEDIATE")
	if !isLockContention(err) {
		t.Fatalf("contender error = %v, want SQLITE_BUSY", err)
	}
	return err
}
