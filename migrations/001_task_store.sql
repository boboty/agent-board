-- Agent Board thin task store.
-- Task-level state is an explicit recorded column. Nothing in this schema
-- derives state from facts, events, or runtime activity. A task has no
-- lifecycle state until it is queued; queueing records READY.

-- One row binding this database to a project identity, plus the READY queue
-- version that guards reorders against lost updates.
CREATE TABLE board (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    project_id TEXT NOT NULL CHECK (length(project_id) = 26),
    ready_version INTEGER NOT NULL CHECK (ready_version >= 1),
    created_at TEXT NOT NULL
) STRICT;

CREATE TABLE tasks (
    id TEXT PRIMARY KEY CHECK (length(id) = 26),
    number INTEGER NOT NULL UNIQUE CHECK (number >= 1),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    description TEXT NOT NULL,
    acceptance_criteria TEXT NOT NULL,
    state TEXT CHECK (state IS NULL OR state IN ('READY', 'IN_PROGRESS', 'DONE', 'BLOCKED')),
    state_reason TEXT,
    queued_at TEXT,
    ready_rank INTEGER,
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK ((queued_at IS NULL) = (ready_rank IS NULL)),
    CHECK ((queued_at IS NULL) = (state IS NULL))
) STRICT;

CREATE UNIQUE INDEX tasks_ready_rank ON tasks(ready_rank) WHERE ready_rank IS NOT NULL;
CREATE INDEX tasks_state ON tasks(state, number);

CREATE TABLE task_facts (
    id TEXT PRIMARY KEY CHECK (length(id) = 26),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    kind TEXT NOT NULL CHECK (length(kind) > 0),
    body TEXT NOT NULL CHECK (length(trim(body)) > 0),
    data TEXT CHECK (data IS NULL OR (json_valid(data) AND json_type(data) = 'object')),
    actor TEXT NOT NULL CHECK (length(actor) > 0),
    created_at TEXT NOT NULL
) STRICT;

CREATE INDEX task_facts_task ON task_facts(task_id, created_at, id);

CREATE TABLE task_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT REFERENCES tasks(id),
    type TEXT NOT NULL CHECK (length(type) > 0),
    actor TEXT NOT NULL CHECK (length(actor) > 0),
    task_version INTEGER,
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    created_at TEXT NOT NULL
) STRICT;

CREATE INDEX task_events_task ON task_events(task_id, id);

CREATE TRIGGER task_facts_append_only_update BEFORE UPDATE ON task_facts
BEGIN SELECT RAISE(ABORT, 'task_facts are append-only'); END;
CREATE TRIGGER task_facts_append_only_delete BEFORE DELETE ON task_facts
BEGIN SELECT RAISE(ABORT, 'task_facts are append-only'); END;
CREATE TRIGGER task_events_append_only_update BEFORE UPDATE ON task_events
BEGIN SELECT RAISE(ABORT, 'task_events are append-only'); END;
CREATE TRIGGER task_events_append_only_delete BEFORE DELETE ON task_events
BEGIN SELECT RAISE(ABORT, 'task_events are append-only'); END;

CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (length(operation) > 0),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) > 0),
    request_hash BLOB NOT NULL CHECK (length(request_hash) = 32),
    response_json TEXT NOT NULL CHECK (json_valid(response_json)),
    created_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
) STRICT;
