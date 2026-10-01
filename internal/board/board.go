// Package board is Agent Board's application contract: the task ledger
// operations exposed to adapters (CLI, MCP, HTTP), implemented over SQLite.
//
// The Board provides capabilities, not rules. Every mutation is recorded as
// requested; whether a role should request it is defined by the Workflow
// Skill. In particular, task state changes only through SetTaskState and is
// never inferred from facts, events, or runtime activity.
//
// Every mutation runs in one BEGIN IMMEDIATE transaction that performs the
// optimistic version check, the change, its audit event, and the idempotency
// record together, so they commit or roll back as a unit.
package board

import (
	"context"
	"crypto/rand"
	"errors"
	"io"

	"github.com/boboty/agent-board/internal/clock"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ids"
	"github.com/boboty/agent-board/internal/sqlite"
	"github.com/boboty/agent-board/migrations"
)

// Config opens a Board database. DatabasePath's parent directory must exist.
type Config struct {
	DatabasePath string
	// ProjectID is the project identity (a ULID) this database belongs to.
	// The first Open binds an empty database to it; later Opens must match.
	ProjectID string
	// Clock and Entropy default to the system clock and crypto/rand.
	Clock   clock.Clock
	Entropy io.Reader
	// SQLite overrides storage options such as the BUSY retry policy.
	SQLite sqlite.Options
}

// Service is an open Board. It is safe for concurrent use, and any number of
// Services in any number of processes may share one database file.
type Service struct {
	db    *sqlite.DB
	clock clock.Clock
	ids   *ids.Generator
}

// Open opens the database, applies pending migrations, and binds or verifies
// the project identity.
func Open(ctx context.Context, config Config) (*Service, error) {
	if _, err := ids.ParseStrict(config.ProjectID); err != nil {
		return nil, domain.Invalid("project_id", "must be a canonical ULID")
	}
	clk := config.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}
	entropy := config.Entropy
	if entropy == nil {
		entropy = rand.Reader
	}
	generator, err := ids.NewGenerator(clk, entropy)
	if err != nil {
		return nil, err
	}
	db, err := sqlite.Open(ctx, config.DatabasePath, config.SQLite)
	if err != nil {
		return nil, err
	}
	service := &Service{db: db, clock: clk, ids: generator}
	if _, err := migrations.Migrate(ctx, db, clk); err != nil {
		return nil, errors.Join(err, db.Close(ctx))
	}
	if err := service.bindProject(ctx, config.ProjectID); err != nil {
		return nil, errors.Join(err, db.Close(ctx))
	}
	return service, nil
}

// Close releases the database.
func (s *Service) Close(ctx context.Context) error {
	return s.db.Close(ctx)
}

func (s *Service) bindProject(ctx context.Context, projectID string) error {
	return s.db.Write(ctx, func(ctx context.Context, tx sqlite.Executor) error {
		var stored string
		err := tx.QueryRowContext(ctx, "SELECT project_id FROM board WHERE singleton = 1").Scan(&stored)
		switch {
		case err == nil:
			if stored != projectID {
				return domain.NewError(domain.CodeProjectMismatch, "database belongs to project "+stored+", not "+projectID, false)
			}
			return nil
		case isNoRows(err):
			_, err := tx.ExecContext(ctx, "INSERT INTO board(singleton, project_id, ready_version, created_at) VALUES (1, ?, 1, ?)",
				projectID, sqlite.FormatTime(s.clock.Now()))
			return err
		default:
			return err
		}
	})
}
