package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/boboty/agent-board/internal/clock"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/sqlite"

	_ "modernc.org/sqlite"
)

var migrationTime = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestEmbeddedChecksumsMatchFiles(t *testing.T) {
	for _, item := range embeddedCatalog {
		contents, err := os.ReadFile(sqlFileName(item))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(contents)
		if got := hex.EncodeToString(sum[:]); got != item.checksum {
			t.Fatalf("migration %d checksum = %s, catalog has %s", item.version, got, item.checksum)
		}
	}
}

func sqlFileName(item migration) string {
	return fmt.Sprintf("%03d_%s.sql", item.version, item.name)
}

func TestMigrateIsIdempotent(t *testing.T) {
	_, db := openDB(t)
	first, err := Migrate(context.Background(), db, clock.NewFakeClock(migrationTime))
	if err != nil || first.Applied != CurrentVersion() || first.Version != CurrentVersion() {
		t.Fatalf("first Migrate() = %+v, %v", first, err)
	}
	second, err := Migrate(context.Background(), db, clock.NewFakeClock(migrationTime))
	if err != nil || second.Applied != 0 || second.Version != CurrentVersion() {
		t.Fatalf("second Migrate() = %+v, %v", second, err)
	}
}

func TestFactProvenanceMigrationKeepsLegacyFactsWithoutInference(t *testing.T) {
	path, db := openDB(t)
	if result, err := run(context.Background(), db, clock.NewFakeClock(migrationTime), embeddedCatalog[:1]); err != nil || result.Version != 1 {
		t.Fatalf("initial migration = %+v, %v", result, err)
	}
	inspect, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer inspect.Close()
	const created = "2026-10-01T12:00:00Z"
	if _, err := inspect.Exec(`INSERT INTO board(singleton, project_id, ready_version, created_at) VALUES (1, ?, 1, ?)`, "01234567890123456789012345", created); err != nil {
		t.Fatal(err)
	}
	if _, err := inspect.Exec(`INSERT INTO tasks(id, number, title, description, acceptance_criteria, state, state_reason, queued_at, ready_rank, version, created_at, updated_at)
		VALUES (?, 1, 'legacy', '', '', NULL, NULL, NULL, NULL, 1, ?, ?)`, "01234567890123456789012346", created, created); err != nil {
		t.Fatal(err)
	}
	if _, err := inspect.Exec(`INSERT INTO task_facts(id, task_id, kind, body, data, actor, created_at)
		VALUES (?, ?, 'execution', 'worker/codex session=old', '{"session":"legacy-data"}', 'worker/codex', ?)`, "01234567890123456789012347", "01234567890123456789012346", created); err != nil {
		t.Fatal(err)
	}
	if result, err := Migrate(context.Background(), db, clock.NewFakeClock(migrationTime)); err != nil || result.Version != 2 || result.Applied != 1 {
		t.Fatalf("provenance migration = %+v, %v", result, err)
	}
	var kind, body, actor string
	var data, role, session, harness, model sql.NullString
	if err := inspect.QueryRow(`SELECT kind, body, data, actor, provenance_role, provenance_session, provenance_harness, provenance_model FROM task_facts WHERE id = ?`, "01234567890123456789012347").Scan(&kind, &body, &data, &actor, &role, &session, &harness, &model); err != nil {
		t.Fatal(err)
	}
	if kind != "execution" || body != "worker/codex session=old" || actor != "worker/codex" || !data.Valid || role.Valid || session.Valid || harness.Valid || model.Valid {
		t.Fatalf("legacy fact changed or provenance inferred: kind=%q body=%q actor=%q data=%v provenance=%v/%v/%v/%v", kind, body, actor, data, role, session, harness, model)
	}
}

func TestConcurrentRunnersHaveOneMigrationOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	const runners = 4
	results := make(chan Result, runners)
	errs := make(chan error, runners)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < runners; i++ {
		db, err := sqlite.Open(context.Background(), path, sqlite.Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close(context.Background()) })
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := Migrate(context.Background(), db, clock.NewFakeClock(migrationTime))
			results <- result
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Migrate() error = %v", err)
		}
	}
	applied := 0
	for result := range results {
		applied += result.Applied
	}
	if applied != CurrentVersion() {
		t.Fatalf("total applied = %d, want %d", applied, CurrentVersion())
	}
}

func TestMigrateRejectsTamperedOrNewerHistory(t *testing.T) {
	for name, tamper := range map[string]string{
		"checksum":  "UPDATE schema_migrations SET checksum = 'aaaa'",
		"name":      "UPDATE schema_migrations SET name = 'renamed_schema'",
		"timestamp": "UPDATE schema_migrations SET applied_at = 'not-a-timestamp'",
		"newer":     "INSERT INTO schema_migrations VALUES (99, 'future_schema', 'x', '2026-01-01T00:00:00Z')",
	} {
		t.Run(name, func(t *testing.T) {
			path, db := openDB(t)
			if _, err := Migrate(context.Background(), db, clock.NewFakeClock(migrationTime)); err != nil {
				t.Fatal(err)
			}
			inspect, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer inspect.Close()
			if _, err := inspect.Exec(tamper); err != nil {
				t.Fatal(err)
			}
			_, err = Migrate(context.Background(), db, clock.NewFakeClock(migrationTime))
			if !domain.IsCode(err, domain.CodeStorageMigration) {
				t.Fatalf("Migrate() error = %v, want STORAGE_MIGRATION", err)
			}
		})
	}
}

func TestBrokenMigrationRollsBackScriptAndHistory(t *testing.T) {
	path, db := openDB(t)
	catalog := []migration{
		testMigration(1, "test_base", "CREATE TABLE base_table (id INTEGER PRIMARY KEY) STRICT;"),
		testMigration(2, "test_broken", "CREATE TABLE must_rollback (id INTEGER) STRICT; INSERT INTO missing_table VALUES (1);"),
	}
	if _, err := run(context.Background(), db, clock.NewFakeClock(migrationTime), catalog); !domain.IsCode(err, domain.CodeStorageMigration) {
		t.Fatalf("run() error = %v, want STORAGE_MIGRATION", err)
	}
	inspect, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer inspect.Close()
	var n int
	if err := inspect.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name IN ('schema_migrations', 'base_table', 'must_rollback')").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d objects survived the failed migration transaction", n)
	}
}

func TestInvalidCatalogRejected(t *testing.T) {
	_, db := openDB(t)
	for name, catalog := range map[string][]migration{
		"empty":    nil,
		"gap":      {testMigration(1, "test_one", "SELECT 1"), testMigration(3, "test_three", "SELECT 1")},
		"checksum": {{version: 1, name: "test_one", checksum: "00", sql: "SELECT 1"}},
		"bad name": {testMigration(1, "Bad Name", "SELECT 1")},
	} {
		if _, err := run(context.Background(), db, clock.NewFakeClock(migrationTime), catalog); !domain.IsCode(err, domain.CodeStorageMigration) {
			t.Errorf("%s: error = %v, want STORAGE_MIGRATION", name, err)
		}
	}
}

func testMigration(version int, name, statement string) migration {
	sum := sha256.Sum256([]byte(statement))
	return migration{version: version, name: name, checksum: hex.EncodeToString(sum[:]), sql: statement}
}

func openDB(t *testing.T) (string, *sqlite.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "board.db")
	db, err := sqlite.Open(context.Background(), path, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	return path, db
}
