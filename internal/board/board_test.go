package board

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/boboty/agent-board/internal/domain"
)

func TestCreateGetListAndUpdateTask(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, CreateTaskInput{Actor: actor, Title: "  Thin store  ", Description: "d", AcceptanceCriteria: "ac"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Number != 1 || task.Title != "Thin store" || task.State != nil || task.Version != 1 ||
		task.QueuedAt != nil || task.ReadyRank != nil || task.Description != "d" || task.AcceptanceCriteria != "ac" {
		t.Fatalf("created task = %+v", task)
	}
	for _, ref := range []string{task.ID, "1", "#1"} {
		if got := mustGet(t, s, ref); !reflect.DeepEqual(got, task) {
			t.Fatalf("GetTask(%q) = %+v, want %+v", ref, got, task)
		}
	}
	_, err = s.GetTask(ctx, "#99")
	wantCode(t, err, domain.CodeTaskNotFound)
	_, err = s.GetTask(ctx, "not-a-ref")
	wantCode(t, err, domain.CodeInvalidArgument)
	_, err = s.CreateTask(ctx, CreateTaskInput{Actor: actor, Title: "   "})
	wantCode(t, err, domain.CodeInvalidArgument)
	_, err = s.CreateTask(ctx, CreateTaskInput{Title: "no actor"})
	wantCode(t, err, domain.CodeInvalidArgument)

	updated, err := s.UpdateTask(ctx, UpdateTaskInput{Actor: actor, Task: "#1", ExpectedVersion: 1, Title: ptr("Thin task store"), Description: ptr("d")})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Thin task store" || updated.Version != 2 || updated.State != nil {
		t.Fatalf("updated task = %+v", updated)
	}
	events := taskEvents(t, s, task.ID)
	if len(events) != 2 || events[1].Type != domain.EventTaskUpdated || *events[1].TaskVersion != 2 || events[1].Actor != actor {
		t.Fatalf("events = %+v", events)
	}
	changes := decodePayload(t, events[1])["changes"].(map[string]any)
	if _, ok := changes["description"]; ok || len(changes) != 1 {
		t.Fatalf("recorded changes = %v, want only title", changes)
	}

	// A request that changes nothing is not a mutation.
	same, err := s.UpdateTask(ctx, UpdateTaskInput{Actor: actor, Task: "#1", ExpectedVersion: 2, Title: ptr("Thin task store")})
	if err != nil || same.Version != 2 || len(taskEvents(t, s, task.ID)) != 2 {
		t.Fatalf("no-op update = %+v, %v", same, err)
	}
	_, err = s.UpdateTask(ctx, UpdateTaskInput{Actor: actor, Task: "#1", ExpectedVersion: 2})
	wantCode(t, err, domain.CodeInvalidArgument)

	second := mustQueue(t, s, mustCreate(t, s, "second"))
	mustSetState(t, s, second, domain.StateDone, nil)
	all, _ := s.ListTasks(ctx, ListTasksInput{})
	done, _ := s.ListTasks(ctx, ListTasksInput{States: []domain.State{domain.StateDone}})
	anyState, _ := s.ListTasks(ctx, ListTasksInput{States: domain.States})
	unqueued, _ := s.ListTasks(ctx, ListTasksInput{Queued: ptr(false)})
	if len(all) != 2 || all[0].Number != 1 || all[1].Number != 2 || len(done) != 1 || done[0].ID != second.ID ||
		len(anyState) != 1 || len(unqueued) != 1 || unqueued[0].ID != task.ID {
		t.Fatalf("list results: all=%d done=%d anyState=%d unqueued=%d", len(all), len(done), len(anyState), len(unqueued))
	}
	_, err = s.ListTasks(ctx, ListTasksInput{States: []domain.State{"REVIEW"}})
	wantCode(t, err, domain.CodeInvalidArgument)
	assertAuditConsistent(t, f)
}

func TestQueueTaskEntersReadyExplicitly(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	a, b, c := mustCreate(t, s, "a"), mustCreate(t, s, "b"), mustCreate(t, s, "c")

	// Unqueued tasks have no lifecycle state and are not in READY.
	version, ready := readyIDs(t, s)
	if version != 1 || len(ready) != 0 || a.State != nil || a.QueuedAt != nil {
		t.Fatalf("fresh READY queue = v%d %v, task a = %+v", version, ready, a)
	}
	_, err := s.SetTaskState(ctx, SetTaskStateInput{Actor: actor, Task: a.ID, ExpectedVersion: a.Version, State: domain.StateReady})
	wantCode(t, err, domain.CodeTaskNotQueued)
	_, err = s.SetTaskState(ctx, SetTaskStateInput{Actor: actor, Task: a.ID, ExpectedVersion: a.Version, State: domain.StateInProgress})
	wantCode(t, err, domain.CodeTaskNotQueued)
	if stored := mustGet(t, s, a.ID); stored.State != nil || stored.Version != 1 || len(taskEvents(t, s, a.ID)) != 1 {
		t.Fatalf("rejected state request changed task: %+v", stored)
	}

	c = mustQueue(t, s, c)
	a = mustQueue(t, s, a)
	if stateOf(c) != domain.StateReady || c.QueuedAt == nil || c.ReadyRank == nil || c.Version != 2 {
		t.Fatalf("queued task = %+v", c)
	}
	events := taskEvents(t, s, c.ID)
	if last := events[len(events)-1]; last.Type != domain.EventTaskQueued || decodePayload(t, last)["state"] != string(domain.StateReady) {
		t.Fatalf("queue event = %+v", last)
	}
	version, ready = readyIDs(t, s)
	if version != 3 || !reflect.DeepEqual(ready, []string{c.ID, a.ID}) {
		t.Fatalf("READY = v%d %v", version, ready)
	}

	_, err = s.QueueTask(ctx, QueueTaskInput{Actor: actor, Task: a.ID, ExpectedVersion: a.Version})
	wantCode(t, err, domain.CodeAlreadyQueued)

	// A queued task that leaves READY drops out of the READY view and
	// reappears in its slot when READY is recorded again.
	b = mustQueue(t, s, b)
	b = mustSetState(t, s, b, domain.StateBlocked, ptr("waiting on API key"))
	_, ready = readyIDs(t, s)
	if !reflect.DeepEqual(ready, []string{c.ID, a.ID}) {
		t.Fatalf("READY = %v, blocked task must not appear", ready)
	}
	_, err = s.QueueTask(ctx, QueueTaskInput{Actor: actor, Task: b.ID, ExpectedVersion: b.Version})
	wantCode(t, err, domain.CodeAlreadyQueued)
	mustSetState(t, s, b, domain.StateReady, nil)
	_, ready = readyIDs(t, s)
	if !reflect.DeepEqual(ready, []string{c.ID, a.ID, b.ID}) {
		t.Fatalf("READY = %v after b returned to READY", ready)
	}

	// Storage enforces that state is present exactly when the task is queued.
	raw := f.raw(t)
	unqueued := mustCreate(t, s, "d")
	for _, statement := range []string{
		"UPDATE tasks SET state = 'READY' WHERE id = '" + unqueued.ID + "'",
		"UPDATE tasks SET state = NULL WHERE id = '" + a.ID + "'",
	} {
		if _, err := raw.Exec(statement); err == nil {
			t.Errorf("%q succeeded; state must be set exactly when queued", statement)
		}
	}
	assertAuditConsistent(t, f)
}

func TestSetTaskStateRecordsAnyOfTheFourStatesExplicitly(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	task := mustQueue(t, s, mustCreate(t, s, "t"))

	// The Board applies no transition policy; every state is reachable from
	// every state, including re-recording the current one.
	sequence := []struct {
		state  domain.State
		reason *string
	}{
		{domain.StateInProgress, nil},
		{domain.StateBlocked, ptr("needs human decision")},
		{domain.StateInProgress, nil},
		{domain.StateDone, nil},
		{domain.StateReady, ptr("reopened")},
		{domain.StateDone, nil},
		{domain.StateDone, nil},
	}
	for i, step := range sequence {
		before := stateOf(task)
		task = mustSetState(t, s, task, step.state, step.reason)
		stored := mustGet(t, s, task.ID)
		if stateOf(stored) != step.state || stored.Version != int64(i+3) || !reflect.DeepEqual(stored.StateReason, step.reason) {
			t.Fatalf("step %d stored = %+v", i, stored)
		}
		events := taskEvents(t, s, task.ID)
		last := events[len(events)-1]
		payload := decodePayload(t, last)
		if last.Type != domain.EventTaskStateSet || payload["from"] != string(before) || payload["to"] != string(step.state) {
			t.Fatalf("step %d event = %s %v", i, last.Type, payload)
		}
	}

	_, err := s.SetTaskState(ctx, SetTaskStateInput{Actor: actor, Task: task.ID, ExpectedVersion: task.Version, State: "IN_REVIEW"})
	wantCode(t, err, domain.CodeInvalidArgument)
	_, err = s.SetTaskState(ctx, SetTaskStateInput{Actor: actor, Task: task.ID, State: domain.StateReady})
	wantCode(t, err, domain.CodeInvalidArgument)

	raw := f.raw(t)
	if _, err := raw.Exec("UPDATE tasks SET state = 'VERIFYING' WHERE id = ?", task.ID); err == nil {
		t.Fatal("storage accepted a fifth state")
	}
	assertAuditConsistent(t, f)
}

func TestVersionConflictNeverSilentlyOverwrites(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	task := mustCreate(t, s, "original")

	// Two writers both read version 1.
	if _, err := s.UpdateTask(ctx, UpdateTaskInput{Actor: "writer-a", Task: task.ID, ExpectedVersion: 1, Title: ptr("from A")}); err != nil {
		t.Fatal(err)
	}
	_, err := s.UpdateTask(ctx, UpdateTaskInput{Actor: "writer-b", Task: task.ID, ExpectedVersion: 1, Title: ptr("from B")})
	wantCode(t, err, domain.CodeVersionConflict)
	_, err = s.SetTaskState(ctx, SetTaskStateInput{Actor: "writer-b", Task: task.ID, ExpectedVersion: 1, State: domain.StateDone})
	wantCode(t, err, domain.CodeVersionConflict)
	_, err = s.QueueTask(ctx, QueueTaskInput{Actor: "writer-b", Task: task.ID, ExpectedVersion: 1})
	wantCode(t, err, domain.CodeVersionConflict)

	stored := mustGet(t, s, task.ID)
	if stored.Title != "from A" || stored.State != nil || stored.Version != 2 || stored.QueuedAt != nil {
		t.Fatalf("stored = %+v; a conflicting write leaked", stored)
	}
	if n := len(taskEvents(t, s, task.ID)); n != 2 {
		t.Fatalf("events = %d, want 2: rejected writes must not be audited as mutations", n)
	}
	assertAuditConsistent(t, f)
}

func TestReorderReadyIsAtomicAndGuarded(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	var tasks []domain.Task
	for _, title := range []string{"a", "b", "c", "d"} {
		tasks = append(tasks, mustQueue(t, s, mustCreate(t, s, title)))
	}
	a, b, c, d := tasks[0], tasks[1], tasks[2], tasks[3]
	// b is queued but IN_PROGRESS: hidden from READY, keeps its rank.
	b = mustSetState(t, s, b, domain.StateInProgress, nil)
	version, ready := readyIDs(t, s)
	if !reflect.DeepEqual(ready, []string{a.ID, c.ID, d.ID}) {
		t.Fatalf("READY = %v", ready)
	}

	for name, order := range map[string][]string{
		"missing member": {d.ID, a.ID},
		"duplicate":      {d.ID, a.ID, a.ID},
		"non-READY task": {d.ID, a.ID, c.ID, b.ID},
		"unqueued task":  {d.ID, a.ID, mustCreate(t, s, "x").ID},
	} {
		_, err := s.ReorderReady(ctx, ReorderReadyInput{Actor: actor, ExpectedVersion: version, Tasks: order})
		if !domain.IsCode(err, domain.CodeReadyOrderConflict) {
			t.Fatalf("%s: error = %v", name, err)
		}
	}
	_, err := s.ReorderReady(ctx, ReorderReadyInput{Actor: actor, ExpectedVersion: version - 1, Tasks: []string{d.ID, a.ID, c.ID}})
	wantCode(t, err, domain.CodeReadyOrderConflict)
	if _, unchanged := readyIDs(t, s); !reflect.DeepEqual(unchanged, ready) {
		t.Fatalf("rejected reorder changed order to %v", unchanged)
	}

	queue, err := s.ReorderReady(ctx, ReorderReadyInput{Actor: actor, ExpectedVersion: version, Tasks: []string{"#4", a.ID, "3"}})
	if err != nil {
		t.Fatal(err)
	}
	if queue.Version != version+1 || len(queue.Tasks) != 3 || queue.Tasks[0].ID != d.ID || queue.Tasks[1].ID != a.ID || queue.Tasks[2].ID != c.ID {
		t.Fatalf("reordered queue = %+v", queue)
	}
	// A concurrent reorder based on the old version loses instead of overwriting.
	_, err = s.ReorderReady(ctx, ReorderReadyInput{Actor: "other", ExpectedVersion: version, Tasks: []string{c.ID, a.ID, d.ID}})
	wantCode(t, err, domain.CodeReadyOrderConflict)

	// b keeps its original slot among the queue's ranks.
	mustSetState(t, s, b, domain.StateReady, nil)
	_, ready = readyIDs(t, s)
	if !reflect.DeepEqual(ready, []string{d.ID, b.ID, a.ID, c.ID}) {
		t.Fatalf("READY after b returned = %v", ready)
	}
	events, _ := s.ListEvents(ctx, ListEventsInput{Limit: MaxEventLimit})
	var reorders int
	for _, event := range events {
		if event.Type == domain.EventReadyReordered {
			reorders++
			if event.TaskID != nil {
				t.Fatal("reorder event should be board-level")
			}
		}
	}
	if reorders != 1 {
		t.Fatalf("reorder events = %d, want 1", reorders)
	}
	// Reorder changes rank only; task versions are governed by their own mutations.
	if got := mustGet(t, s, a.ID); got.Version != a.Version {
		t.Fatalf("reorder changed task version %d -> %d", a.Version, got.Version)
	}
	assertAuditConsistent(t, f)
}

func TestIdempotentRetriesApplyOnce(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	in := CreateTaskInput{Actor: actor, IdempotencyKey: "create-1", Title: "once"}
	first, err := s.CreateTask(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.CreateTask(ctx, in)
	if err != nil || !reflect.DeepEqual(replay, first) {
		t.Fatalf("replay = %+v, %v; want %+v", replay, err, first)
	}
	if all, _ := s.ListTasks(ctx, ListTasksInput{}); len(all) != 1 {
		t.Fatalf("tasks = %d, want 1", len(all))
	}
	_, err = s.CreateTask(ctx, CreateTaskInput{Actor: actor, IdempotencyKey: "create-1", Title: "different"})
	wantCode(t, err, domain.CodeIdempotencyConflict)

	// A replayed state change returns its original result even after later
	// mutations, and does not apply again.
	mustQueue(t, s, first)
	setIn := SetTaskStateInput{Actor: actor, IdempotencyKey: "start-1", Task: first.ID, ExpectedVersion: 2, State: domain.StateInProgress}
	started, err := s.SetTaskState(ctx, setIn)
	if err != nil {
		t.Fatal(err)
	}
	mustSetState(t, s, started, domain.StateDone, nil)
	again, err := s.SetTaskState(ctx, setIn)
	if err != nil || !reflect.DeepEqual(again, started) {
		t.Fatalf("replayed set state = %+v, %v", again, err)
	}
	if stored := mustGet(t, s, first.ID); stateOf(stored) != domain.StateDone || stored.Version != 4 {
		t.Fatalf("stored after replay = %+v", stored)
	}

	factIn := RecordFactInput{Actor: actor, IdempotencyKey: "fact-1", Task: first.ID, Kind: domain.FactDelivery, Body: "PR opened", Baseline: ptr("base"), Fingerprint: ptr("fingerprint")}
	f1, err := s.RecordFact(ctx, factIn)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := s.RecordFact(ctx, factIn)
	if err != nil || f1.ID != f2.ID {
		t.Fatalf("fact replay = %+v, %v", f2, err)
	}
	if facts, _ := s.ListFacts(ctx, ListFactsInput{Task: first.ID}); len(facts) != 1 {
		t.Fatalf("facts = %d, want 1", len(facts))
	}
	// Keys are scoped per operation.
	if _, err := s.RecordFact(ctx, RecordFactInput{Actor: actor, IdempotencyKey: "create-1", Task: first.ID, Kind: domain.FactNote, Body: "n"}); err != nil {
		t.Fatalf("same key on another operation: %v", err)
	}
	assertAuditConsistent(t, f)
}

func TestFactsAreRecordedAndNeverChangeState(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	task := mustSetState(t, s, mustQueue(t, s, mustCreate(t, s, "t")), domain.StateInProgress, nil)

	// Facts that a workflow engine might be tempted to interpret. The Board
	// must record them and leave state alone.
	facts := []RecordFactInput{
		{Kind: domain.FactExecution, Body: "developer started", Data: json.RawMessage(`{"harness":"claude-code", "worktree":"../wt-ab1", "model":"opus", "lease_expires_at":"2020-01-01T00:00:00Z"}`)},
		{Kind: domain.FactDelivery, Body: "delivered", Data: json.RawMessage(`{"commit":"abc123"}`), Baseline: ptr("base"), Fingerprint: ptr("fingerprint"), Provenance: &domain.FactProvenance{Role: "worker", Session: "same-session", Harness: "codex", Model: "gpt-6"}},
		{Kind: domain.FactVerification, Body: "RC: missing test", Data: json.RawMessage(`{"result":"RC"}`), Verdict: ptr("RC"), Baseline: ptr("base"), Fingerprint: ptr("fingerprint"), Provenance: &domain.FactProvenance{Role: "verifier", Session: "same-session", Harness: "codex", Model: "gpt-6"}},
		{Kind: domain.FactVerification, Body: "PASS", Data: json.RawMessage(`{"result":"PASS"}`), Verdict: ptr("PASS"), Baseline: ptr("base"), Fingerprint: ptr("fingerprint")},
		{Kind: domain.FactDecision, Body: "Human accepts without verifier PASS", Data: json.RawMessage(`{"decision":"accept","reason":"accepted by Human"}`)},
		{Kind: domain.FactHandoff, Body: "handing off to new session"},
		{Kind: domain.FactNote, Body: "blocked? no, just a note mentioning BLOCKED and DONE"},
	}
	for _, in := range facts {
		in.Actor, in.Task = actor, task.ID
		if _, err := s.RecordFact(ctx, in); err != nil {
			t.Fatalf("RecordFact(%s) error = %v", in.Kind, err)
		}
		stored := mustGet(t, s, task.ID)
		if stateOf(stored) != domain.StateInProgress || stored.Version != task.Version || !stored.UpdatedAt.Equal(task.UpdatedAt) {
			t.Fatalf("recording a %s fact changed the task: %+v", in.Kind, stored)
		}
	}
	all, err := s.ListFacts(ctx, ListFactsInput{Task: task.ID})
	if err != nil || len(all) != len(facts) {
		t.Fatalf("facts = %d, %v", len(all), err)
	}
	if string(all[1].Data) != `{"commit":"abc123"}` || all[0].Kind != domain.FactExecution || all[5].Data != nil {
		t.Fatalf("stored facts = %+v", all)
	}
	if all[1].Provenance == nil || all[2].Provenance == nil || all[1].Provenance.Session != all[2].Provenance.Session || all[1].Provenance.Role != "worker" || all[2].Provenance.Role != "verifier" {
		t.Fatalf("fact provenance was not preserved (same-session facts must remain recordable): %+v", all[1:3])
	}
	if all[1].Baseline == nil || *all[1].Baseline != "base" || all[1].Fingerprint == nil || all[2].Verdict == nil || *all[2].Verdict != "RC" || all[2].Baseline == nil || all[2].Fingerprint == nil {
		t.Fatalf("structured core fields missing from list_facts: %+v", all[1:3])
	}
	if all[0].Provenance != nil || all[3].Provenance != nil {
		t.Fatalf("missing provenance was inferred: %+v", all)
	}
	verifications, _ := s.ListFacts(ctx, ListFactsInput{Task: task.ID, Kind: domain.FactVerification})
	if len(verifications) != 2 {
		t.Fatalf("verification facts = %d", len(verifications))
	}
	decisions, err := s.ListFacts(ctx, ListFactsInput{Task: task.ID, Kind: domain.FactDecision})
	if err != nil || len(decisions) != 1 || decisions[0].Kind != domain.FactDecision {
		t.Fatalf("decision facts = %+v, %v", decisions, err)
	}

	for name, in := range map[string]RecordFactInput{
		"unknown kind":   {Kind: "review", Body: "x"},
		"empty body":     {Kind: domain.FactNote, Body: " "},
		"array data":     {Kind: domain.FactNote, Body: "x", Data: json.RawMessage(`[1]`)},
		"malformed data": {Kind: domain.FactNote, Body: "x", Data: json.RawMessage(`{`)},
	} {
		in.Actor, in.Task = actor, task.ID
		if _, err := s.RecordFact(ctx, in); !domain.IsCode(err, domain.CodeInvalidArgument) {
			t.Errorf("%s: error = %v", name, err)
		}
	}

	raw := f.raw(t)
	for _, statement := range []string{"UPDATE task_facts SET body = 'edited'", "DELETE FROM task_facts", "UPDATE task_events SET actor = 'x'", "DELETE FROM task_events"} {
		if _, err := raw.Exec(statement); err == nil {
			t.Errorf("%q succeeded; facts and events must be append-only", statement)
		}
	}
	assertAuditConsistent(t, f)
}

func TestDeliveryAndVerificationCoreSemanticsAreRequiredAndTyped(t *testing.T) {
	s, _ := newService(t)
	task := mustQueue(t, s, mustCreate(t, s, "core semantics"))
	valid := []RecordFactInput{
		{Kind: domain.FactDelivery, Body: "delivered", Baseline: ptr(" base "), Fingerprint: ptr(" hash "), AcceptedCommit: ptr(" commit ")},
		{Kind: domain.FactVerification, Body: "verified", Verdict: ptr("RC"), Baseline: ptr("base"), Fingerprint: ptr("hash")},
	}
	for _, in := range valid {
		in.Actor, in.Task = actor, task.ID
		stored, err := s.RecordFact(context.Background(), in)
		if err != nil {
			t.Fatalf("RecordFact(%s): %v", in.Kind, err)
		}
		if in.Kind == domain.FactDelivery && (stored.Baseline == nil || *stored.Baseline != "base" || stored.Fingerprint == nil || *stored.Fingerprint != "hash" || stored.AcceptedCommit == nil || *stored.AcceptedCommit != "commit") {
			t.Fatalf("delivery fields %+v", stored)
		}
		if in.Kind == domain.FactVerification && (stored.Verdict == nil || *stored.Verdict != "RC" || stored.Baseline == nil || stored.Fingerprint == nil) {
			t.Fatalf("verification fields %+v", stored)
		}
	}
	invalid := []RecordFactInput{
		{Kind: domain.FactDelivery, Body: "x", Fingerprint: ptr("hash")},
		{Kind: domain.FactDelivery, Body: "x", Baseline: ptr("base"), Fingerprint: ptr(" ")},
		{Kind: domain.FactDelivery, Body: "x", Baseline: ptr("base"), Fingerprint: ptr("hash"), AcceptedCommit: ptr("")},
		{Kind: domain.FactVerification, Body: "x", Verdict: ptr("PASS"), Baseline: ptr("base")},
		{Kind: domain.FactVerification, Body: "x", Verdict: ptr("pass"), Baseline: ptr("base"), Fingerprint: ptr("hash")},
		{Kind: domain.FactVerification, Body: "x", Verdict: ptr("RC"), Baseline: ptr("base"), Fingerprint: ptr("hash"), AcceptedCommit: ptr("commit")},
	}
	for _, in := range invalid {
		in.Actor, in.Task = actor, task.ID
		if _, err := s.RecordFact(context.Background(), in); !domain.IsCode(err, domain.CodeInvalidArgument) {
			t.Errorf("RecordFact(%s %+v) error=%v, want INVALID_ARGUMENT", in.Kind, in, err)
		}
	}
}

func TestLegacyFactCoreFieldsStayMissingOnRead(t *testing.T) {
	s, f := newService(t)
	task := mustCreate(t, s, "legacy fact")
	_, err := f.raw(t).Exec(`INSERT INTO task_facts(id, task_id, kind, body, data, actor, created_at)
		VALUES (?, ?, 'verification', 'PASS according to old body', '{"verdict":"PASS","baseline":"old-base","fingerprint":"old-hash"}', 'legacy', ?)`,
		"01M3VN4DT676SGJ90T58JRB13X", task.ID, "2026-10-01T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	facts, err := s.ListFacts(context.Background(), ListFactsInput{Task: task.ID})
	if err != nil || len(facts) != 1 {
		t.Fatalf("ListFacts = %+v, %v", facts, err)
	}
	if facts[0].Baseline != nil || facts[0].Fingerprint != nil || facts[0].Verdict != nil || string(facts[0].Data) != `{"verdict":"PASS","baseline":"old-base","fingerprint":"old-hash"}` {
		t.Fatalf("legacy semantics were inferred or evidence changed: %+v", facts[0])
	}
}

// TestMutationAndEventCommitTogether injects a failure into the audit insert
// and checks that the mutation, the idempotency record, and the event all roll
// back together.
func TestMutationAndEventCommitTogether(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	queued := mustQueue(t, s, mustCreate(t, s, "queued"))
	task := mustCreate(t, s, "t")
	raw := f.raw(t)
	if _, err := raw.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON task_events
		WHEN NEW.type IN ('task_state_set', 'fact_recorded', 'task_created', 'task_queued')
		BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`); err != nil {
		t.Fatal(err)
	}

	_, err := s.SetTaskState(ctx, SetTaskStateInput{Actor: actor, IdempotencyKey: "k", Task: queued.ID, ExpectedVersion: 2, State: domain.StateDone})
	wantCode(t, err, domain.CodeStorageConstraint)
	_, err = s.QueueTask(ctx, QueueTaskInput{Actor: actor, Task: task.ID, ExpectedVersion: 1})
	wantCode(t, err, domain.CodeStorageConstraint)
	_, err = s.RecordFact(ctx, RecordFactInput{Actor: actor, Task: task.ID, Kind: domain.FactNote, Body: "x"})
	wantCode(t, err, domain.CodeStorageConstraint)
	_, err = s.CreateTask(ctx, CreateTaskInput{Actor: actor, Title: "never"})
	wantCode(t, err, domain.CodeStorageConstraint)

	if stored := mustGet(t, s, queued.ID); stateOf(stored) != domain.StateReady || stored.Version != 2 {
		t.Fatalf("partial commit of state change: %+v", stored)
	}
	if stored := mustGet(t, s, task.ID); stored.State != nil || stored.Version != 1 || stored.QueuedAt != nil {
		t.Fatalf("partial commit of queue: %+v", stored)
	}
	var facts, tasks, keys, readyVersion int
	_ = raw.QueryRow("SELECT count(*) FROM task_facts").Scan(&facts)
	_ = raw.QueryRow("SELECT count(*) FROM tasks").Scan(&tasks)
	_ = raw.QueryRow("SELECT count(*) FROM idempotency_records").Scan(&keys)
	_ = raw.QueryRow("SELECT ready_version FROM board").Scan(&readyVersion)
	if facts != 0 || tasks != 2 || keys != 0 || readyVersion != 2 {
		t.Fatalf("partial commit: facts=%d tasks=%d keys=%d ready_version=%d", facts, tasks, keys, readyVersion)
	}

	if _, err := raw.Exec("DROP TRIGGER fail_audit"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTaskState(ctx, SetTaskStateInput{Actor: actor, IdempotencyKey: "k", Task: queued.ID, ExpectedVersion: 2, State: domain.StateDone}); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	assertAuditConsistent(t, f)
}

func TestListEventsPagesInCommitOrder(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	a := mustCreate(t, s, "a")
	b := mustCreate(t, s, "b")
	mustQueue(t, s, a)

	page1, err := s.ListEvents(ctx, ListEventsInput{Limit: 2})
	if err != nil || len(page1) != 2 {
		t.Fatalf("page1 = %d, %v", len(page1), err)
	}
	page2, _ := s.ListEvents(ctx, ListEventsInput{AfterID: page1[1].ID, Limit: 2})
	if len(page2) != 1 || page2[0].Type != domain.EventTaskQueued || page2[0].ID <= page1[1].ID {
		t.Fatalf("page2 = %+v", page2)
	}
	if events := taskEvents(t, s, "#2"); len(events) != 1 || *events[0].TaskID != b.ID {
		t.Fatalf("task #2 events = %+v", events)
	}
	_, err = s.ListEvents(ctx, ListEventsInput{Limit: MaxEventLimit + 1})
	wantCode(t, err, domain.CodeInvalidArgument)
}

func TestDatabaseIsBoundToOneProject(t *testing.T) {
	f := newFixture(t)
	s := f.open(t)
	mustCreate(t, s, "t")

	other := newFixture(t)
	_, err := Open(context.Background(), Config{DatabasePath: f.path, ProjectID: other.projectID})
	wantCode(t, err, domain.CodeProjectMismatch)
	_, err = Open(context.Background(), Config{DatabasePath: f.path, ProjectID: "not-a-ulid"})
	wantCode(t, err, domain.CodeInvalidArgument)

	reopened := f.open(t)
	if got := mustGet(t, reopened, "#1"); got.Title != "t" {
		t.Fatalf("reopened task = %+v", got)
	}
}
