package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/boboty/agent-board/internal/board"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ops"
)

const testProjectID = "01M3VN4DT676SGJ90T58JRB13R"

type fixture struct {
	t       *testing.T
	ops     *ops.Service
	handler http.Handler
	csrf    string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	service, err := board.Open(ctx, board.Config{DatabasePath: filepath.Join(t.TempDir(), "board.db"), ProjectID: testProjectID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close(ctx) })
	f := &fixture{t: t, ops: ops.New(service, "web")}
	f.handler, err = NewHandler(f.ops, Info{ProjectID: testProjectID, Actor: "web"})
	if err != nil {
		t.Fatal(err)
	}
	page := f.get("/").body
	match := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page)
	if match == nil {
		t.Fatal("page has no csrf token")
	}
	f.csrf = match[1]
	return f
}

type response struct {
	status int
	header http.Header
	body   string
}

func (f *fixture) do(request *http.Request) response {
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	body, _ := io.ReadAll(recorder.Result().Body)
	return response{status: recorder.Code, header: recorder.Result().Header, body: string(body)}
}

func (f *fixture) get(target string) response {
	return f.do(httptest.NewRequest(http.MethodGet, target, nil))
}

// post submits a form the way a same-origin browser does, with the CSRF
// token unless the form sets its own.
func (f *fixture) post(target string, form url.Values, headers ...string) response {
	if !form.Has("csrf_token") {
		form.Set("csrf_token", f.csrf)
	}
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "http://example.com")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i+1] == "" {
			request.Header.Del(headers[i])
		} else {
			request.Header.Set(headers[i], headers[i+1])
		}
	}
	return f.do(request)
}

// redirect asserts a 303 and returns the target query.
func (r response) redirect(t *testing.T) url.Values {
	t.Helper()
	if r.status != http.StatusSeeOther {
		t.Fatalf("status %d, want 303; body %q", r.status, r.body)
	}
	target, err := url.Parse(r.header.Get("Location"))
	if err != nil || target.Path != "/" {
		t.Fatalf("redirect to %q", r.header.Get("Location"))
	}
	return target.Query()
}

func (f *fixture) task(ref string) domain.Task {
	f.t.Helper()
	result, err := f.ops.GetTask(context.Background(), ops.GetTaskArgs{Task: ref})
	if err != nil {
		f.t.Fatal(err)
	}
	return result.Task
}

func (f *fixture) create(title string) domain.Task {
	f.t.Helper()
	result, err := f.ops.CreateTask(context.Background(), ops.CreateTaskArgs{Title: title})
	if err != nil {
		f.t.Fatal(err)
	}
	return result.Task
}

func (f *fixture) queue(task domain.Task) domain.Task {
	f.t.Helper()
	result, err := f.ops.QueueTask(context.Background(), ops.QueueTaskArgs{Task: task.ID, ExpectedVersion: task.Version})
	if err != nil {
		f.t.Fatal(err)
	}
	return result.Task
}

func (f *fixture) setState(task domain.Task, state domain.State) domain.Task {
	f.t.Helper()
	result, err := f.ops.SetTaskState(context.Background(), ops.SetTaskStateArgs{Task: task.ID, ExpectedVersion: task.Version, State: state})
	if err != nil {
		f.t.Fatal(err)
	}
	return result.Task
}

func (f *fixture) events() []domain.TaskEvent {
	f.t.Helper()
	result, err := f.ops.ListEvents(context.Background(), ops.ListEventsArgs{Limit: board.MaxEventLimit})
	if err != nil {
		f.t.Fatal(err)
	}
	return result.Events
}

// boardLayout parses the live region into column -> task numbers in display
// order, plus the unqueued list under the key "UNQUEUED".
func boardLayout(t *testing.T, html string) map[string][]int64 {
	t.Helper()
	layout := map[string][]int64{}
	sections := regexp.MustCompile(`data-column="([A-Z_]+)"|data-unqueued|class="drawer-overlay"`).FindAllStringSubmatchIndex(html, -1)
	taskRe := regexp.MustCompile(`<(?:article|div) class="board-(?:card|row)" data-task="(\d+)"`)
	for i, section := range sections {
		end := len(html)
		if i+1 < len(sections) {
			end = sections[i+1][0]
		}
		name := "UNQUEUED"
		switch {
		case section[2] >= 0:
			name = html[section[2]:section[3]]
		case strings.HasPrefix(html[section[0]:], "class=\"drawer"):
			continue
		}
		layout[name] = []int64{}
		for _, match := range taskRe.FindAllStringSubmatch(html[section[1]:end], -1) {
			number, _ := strconv.ParseInt(match[1], 10, 64)
			layout[name] = append(layout[name], number)
		}
	}
	return layout
}

// form is one rendered form: its action, hidden/text fields, and button.
type form struct {
	action string
	button string
	values url.Values
}

var (
	formRe   = regexp.MustCompile(`(?s)<form[^>]*action="([^"]+)"[^>]*>(.*?)</form>`)
	inputRe  = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)
	buttonRe = regexp.MustCompile(`data-action="([^"]+)"`)
)

// forms extracts the rendered forms so tests submit exactly what the page
// would.
func forms(html string) []form {
	var out []form
	for _, match := range formRe.FindAllStringSubmatch(html, -1) {
		f := form{action: match[1], values: url.Values{}}
		for _, input := range inputRe.FindAllStringSubmatch(match[2], -1) {
			f.values.Add(input[1], input[2])
		}
		if button := buttonRe.FindStringSubmatch(match[2]); button != nil {
			f.button = button[1]
		}
		out = append(out, f)
	}
	return out
}

func findForm(t *testing.T, html, action, button string) form {
	t.Helper()
	for _, f := range forms(html) {
		if f.action == action && f.button == button {
			return f
		}
	}
	t.Fatalf("no form %s [%s]", action, button)
	return form{}
}

func TestColumnsAreExactlyTheRecordedState(t *testing.T) {
	f := newFixture(t)
	ready1 := f.queue(f.create("ready one"))
	progress := f.setState(f.queue(f.create("in progress")), domain.StateInProgress)
	done := f.setState(f.queue(f.create("done")), domain.StateDone)
	blocked := f.setState(f.queue(f.create("blocked")), domain.StateBlocked)
	unqueued := f.create("not queued")
	ready2 := f.queue(f.create("ready two"))

	live := f.get("/live")
	if live.status != http.StatusOK {
		t.Fatalf("status %d", live.status)
	}
	got := boardLayout(t, live.body)
	want := map[string][]int64{
		"READY":       {ready1.Number, ready2.Number},
		"IN_PROGRESS": {progress.Number},
		"DONE":        {done.Number},
		"BLOCKED":     {blocked.Number},
		"UNQUEUED":    {unqueued.Number},
	}
	for name, numbers := range want {
		if !slices.Equal(got[name], numbers) {
			t.Errorf("%s = %v, want %v", name, got[name], numbers)
		}
	}
	if len(got) != len(want) {
		t.Errorf("sections %v, want exactly four columns and the unqueued list", got)
	}
	// The column order is the domain's state order.
	var order []string
	for _, match := range regexp.MustCompile(`data-column="([A-Z_]+)"`).FindAllStringSubmatch(live.body, -1) {
		order = append(order, match[1])
	}
	if !slices.Equal(order, []string{"READY", "IN_PROGRESS", "DONE", "BLOCKED"}) {
		t.Errorf("column order %v", order)
	}

	// A recorded state change moves the card; nothing else does.
	f.setState(f.task(ready1.ID), domain.StateBlocked)
	got = boardLayout(t, f.get("/live").body)
	if !slices.Equal(got["READY"], []int64{ready2.Number}) || !slices.Equal(got["BLOCKED"], []int64{ready1.Number, blocked.Number}) {
		t.Errorf("after set_task_state: %v", got)
	}
}

func TestGroupByStateUsesOnlyStateAndRank(t *testing.T) {
	state := func(s domain.State) *domain.State { return &s }
	rank := func(r int64) *int64 { return &r }
	tasks := []domain.Task{
		{ID: "a", Number: 1, State: state(domain.StateReady), ReadyRank: rank(30)},
		{ID: "b", Number: 2},
		{ID: "c", Number: 3, State: state(domain.StateReady), ReadyRank: rank(10)},
		{ID: "d", Number: 4, State: state(domain.StateDone), ReadyRank: rank(5)},
		{ID: "e", Number: 5, State: state(domain.StateReady), ReadyRank: rank(20)},
	}
	columns, unqueued := groupByState(tasks)
	if len(columns) != 4 || len(unqueued) != 1 || unqueued[0].ID != "b" {
		t.Fatalf("columns %d, unqueued %v", len(columns), unqueued)
	}
	var ready []string
	for _, c := range columns[0].Cards {
		ready = append(ready, c.ID)
	}
	if !slices.Equal(ready, []string{"c", "e", "a"}) {
		t.Fatalf("READY order %v", ready)
	}
	cards := columns[0].Cards
	if cards[0].Up != nil || !slices.Equal(cards[0].Down, []string{"e", "c", "a"}) ||
		!slices.Equal(cards[2].Up, []string{"c", "a", "e"}) || cards[2].Down != nil {
		t.Fatalf("moves %+v", cards)
	}
	if columns[2].State != domain.StateDone || len(columns[2].Cards) != 1 || columns[2].Cards[0].Up != nil {
		t.Fatalf("DONE column %+v", columns[2])
	}
}

func TestWebWritesGoThroughTheBoard(t *testing.T) {
	f := newFixture(t)
	before := len(f.events())

	// Create: an unqueued task with no state, recorded by the web actor.
	query := f.post("/tasks", url.Values{"title": {"  web task  "}, "description": {"line1\r\nline2"}, "idempotency_key": {"k1"}}).redirect(t)
	if query.Get("notice") != "created" || query.Get("task") != "1" {
		t.Fatalf("create redirect %v", query)
	}
	created := f.task("1")
	if created.State != nil || created.QueuedAt != nil || created.Title != "web task" || created.Description != "line1\nline2" {
		t.Fatalf("created %+v", created)
	}
	// Replaying the idempotency key does not create a second task.
	f.post("/tasks", url.Values{"title": {"  web task  "}, "description": {"line1\r\nline2"}, "idempotency_key": {"k1"}}).redirect(t)
	if tasks, _ := f.ops.ListTasks(context.Background(), ops.ListTasksArgs{}); len(tasks.Tasks) != 1 {
		t.Fatalf("idempotent replay created %d tasks", len(tasks.Tasks))
	}
	if layout := boardLayout(t, f.get("/live").body); !slices.Equal(layout["UNQUEUED"], []int64{1}) || len(layout["READY"]) != 0 {
		t.Fatalf("unqueued task leaked into READY: %v", layout)
	}

	// Edit with the version the drawer rendered; then the same stale form fails.
	drawer := f.get("/?task=1").body
	edit := findForm(t, drawer, "/tasks/1", "save")
	edit.values.Set("title", "edited")
	edit.values.Set("description", "line1\r\nline2")
	edit.values.Set("acceptance_criteria", "")
	if query := f.post(edit.action, edit.values).redirect(t); query.Get("notice") != "updated" {
		t.Fatalf("edit %v", query)
	}
	edit.values.Set("title", "stale edit")
	edit.values.Set("idempotency_key", "")
	if query := f.post(edit.action, edit.values).redirect(t); query.Get("error") != domain.CodeVersionConflict || query.Get("task") != "1" {
		t.Fatalf("stale edit %v", query)
	}
	if task := f.task("1"); task.Title != "edited" || task.Version != 2 {
		t.Fatalf("after edits %+v", task)
	}

	// Setting a state before queueing is the Board's TASK_NOT_QUEUED.
	if query := f.post("/tasks/1/state", url.Values{"expected_version": {"2"}, "state": {"DONE"}, "return_task": {"1"}}).redirect(t); query.Get("error") != domain.CodeTaskNotQueued {
		t.Fatalf("state on unqueued %v", query)
	}

	// Queue from the unqueued list, as rendered.
	queue := findForm(t, f.get("/").body, "/tasks/1/queue", "queue")
	if query := f.post(queue.action, queue.values).redirect(t); query.Get("notice") != "queued" || query.Has("task") {
		t.Fatalf("queue %v", query)
	}
	if task := f.task("1"); task.State == nil || *task.State != domain.StateReady {
		t.Fatalf("queued task %+v", task)
	}

	// Set state from the drawer with a reason.
	state := findForm(t, f.get("/?task=1").body, "/tasks/1/state", "set-state")
	state.values.Set("state", "BLOCKED")
	state.values.Set("reason", "waiting\r\nfor decision")
	f.post(state.action, state.values).redirect(t)
	task := f.task("1")
	if *task.State != domain.StateBlocked || *task.StateReason != "waiting\nfor decision" {
		t.Fatalf("state %+v", task)
	}
	if layout := boardLayout(t, f.get("/live").body); !slices.Equal(layout["BLOCKED"], []int64{1}) {
		t.Fatalf("layout after BLOCKED %v", layout)
	}

	// Every successful web write is exactly one audit event by the web actor.
	events := f.events()[before:]
	var types []domain.EventType
	for _, event := range events {
		if event.Actor != "web" {
			t.Errorf("event %s actor %q", event.Type, event.Actor)
		}
		types = append(types, event.Type)
	}
	want := []domain.EventType{domain.EventTaskCreated, domain.EventTaskUpdated, domain.EventTaskQueued, domain.EventTaskStateSet}
	if !slices.Equal(types, want) {
		t.Fatalf("events %v, want %v", types, want)
	}
}

func TestReadyReorderFromRenderedForms(t *testing.T) {
	f := newFixture(t)
	a := f.queue(f.create("a"))
	b := f.queue(f.create("b"))
	c := f.queue(f.create("c"))
	page := f.get("/").body

	// Move c up: c swaps with b.
	up := findForm(t, page, "/ready/order", "move-up")
	var upForC form
	for _, candidate := range forms(page) {
		if candidate.button == "move-up" && slices.Equal(candidate.values["tasks"], []string{a.ID, c.ID, b.ID}) {
			upForC = candidate
		}
	}
	if upForC.action == "" {
		t.Fatalf("no move-up form for c; first was %v", up.values)
	}
	if query := f.post(upForC.action, upForC.values).redirect(t); query.Get("notice") != "reordered" {
		t.Fatalf("reorder %v", query)
	}
	if layout := boardLayout(t, f.get("/live").body); !slices.Equal(layout["READY"], []int64{a.Number, c.Number, b.Number}) {
		t.Fatalf("READY after move %v", layout["READY"])
	}
	queue, _ := f.ops.ListReady(context.Background(), ops.ListReadyArgs{})
	var ids []string
	for _, task := range queue.Tasks {
		ids = append(ids, task.ID)
	}
	if !slices.Equal(ids, []string{a.ID, c.ID, b.ID}) {
		t.Fatalf("list_ready %v", ids)
	}

	// The first, first-row up and last-row down buttons are disabled.
	if strings.Count(page, `data-action="move-up" disabled`) != 1 || strings.Count(page, `data-action="move-down" disabled`) != 1 {
		t.Fatalf("expected one disabled up and one disabled down button")
	}

	// A form rendered before the reorder carries a stale READY version.
	upForC.values.Set("idempotency_key", "")
	if query := f.post(upForC.action, upForC.values).redirect(t); query.Get("error") != domain.CodeReadyOrderConflict {
		t.Fatalf("stale reorder %v", query)
	}
	if layout := boardLayout(t, f.get("/live").body); !slices.Equal(layout["READY"], []int64{a.Number, c.Number, b.Number}) {
		t.Fatalf("stale reorder changed READY %v", layout["READY"])
	}
}

func TestFactsNeverChangeTaskState(t *testing.T) {
	f := newFixture(t)
	task := f.setState(f.queue(f.create("with facts")), domain.StateInProgress)
	layoutBefore := boardLayout(t, f.get("/live").body)
	for _, kind := range domain.FactKinds {
		form := findForm(t, f.get("/?task=1").body, "/tasks/1/facts", "record-fact")
		form.values.Set("kind", string(kind))
		form.values.Set("body", "fact "+string(kind)+" PASS DONE")
		form.values.Set("data", `{"verdict": "PASS", "state": "DONE"}`)
		if query := f.post(form.action, form.values).redirect(t); query.Get("notice") != "fact" || query.Get("task") != "1" {
			t.Fatalf("record %s: %v", kind, query)
		}
	}
	after := f.task(task.ID)
	if *after.State != domain.StateInProgress || after.Version != task.Version || after.StateReason != nil {
		t.Fatalf("facts changed the task: %+v", after)
	}
	if layout := boardLayout(t, f.get("/live").body); !slices.Equal(layout["IN_PROGRESS"], layoutBefore["IN_PROGRESS"]) {
		t.Fatalf("facts moved the card: %v", layout)
	}
	drawer := f.get("/?task=1").body
	if strings.Count(drawer, `class="fact" data-fact-kind=`) != len(domain.FactKinds)+4 {
		t.Fatalf("drawer should list 5 facts plus the latest execution/delivery/verification/handoff summary")
	}
	// Invalid data is the Board's INVALID_ARGUMENT, reported with its field.
	form := findForm(t, drawer, "/tasks/1/facts", "record-fact")
	form.values.Set("kind", "note")
	form.values.Set("body", "x")
	form.values.Set("data", "[1,2]")
	if query := f.post(form.action, form.values).redirect(t); query.Get("error") != domain.CodeInvalidArgument || query.Get("field") != "data" {
		t.Fatalf("invalid data %v", query)
	}
}

func TestDrawerShowsFactsAndHistory(t *testing.T) {
	f := newFixture(t)
	f.queue(f.create("detail"))
	ctx := context.Background()
	if _, err := f.ops.RecordFact(ctx, ops.RecordFactArgs{Write: ops.Write{Actor: "dev-agent"}, Task: "1", Kind: domain.FactDelivery,
		Body: "commit abc123", Data: []byte(`{"commit":"abc123"}`)}); err != nil {
		t.Fatal(err)
	}
	page := f.get("/?task=1")
	if page.status != http.StatusOK {
		t.Fatalf("status %d", page.status)
	}
	for _, want := range []string{`data-detail="1"`, "commit abc123", "dev-agent", `&#34;commit&#34;: &#34;abc123&#34;`,
		`data-event="task_created"`, `data-event="task_queued"`, `data-event="fact_recorded"`, zhCN.Columns[domain.StateReady]} {
		if !strings.Contains(page.body, want) {
			t.Errorf("drawer missing %q", want)
		}
	}
	missing := f.get("/?task=99")
	if missing.status != http.StatusNotFound || !strings.Contains(missing.body, zhCN.Errors[domain.CodeTaskNotFound]) || strings.Contains(missing.body, "data-detail=") {
		t.Fatalf("missing task: status %d", missing.status)
	}
}

func TestWriteSecurity(t *testing.T) {
	f := newFixture(t)
	before := len(f.events())
	title := url.Values{"title": {"x"}}
	cases := []struct {
		name    string
		form    url.Values
		headers []string
		status  int
	}{
		{"missing token", url.Values{"title": {"x"}, "csrf_token": {""}}, nil, http.StatusForbidden},
		{"wrong token", url.Values{"title": {"x"}, "csrf_token": {"nope"}}, nil, http.StatusForbidden},
		{"cross-site fetch metadata", title, []string{"Sec-Fetch-Site", "cross-site"}, http.StatusForbidden},
		{"same-site other origin", title, []string{"Sec-Fetch-Site", "same-site"}, http.StatusForbidden},
		{"foreign origin", title, []string{"Origin", "http://evil.example"}, http.StatusForbidden},
		{"null origin cross-site", title, []string{"Origin", "null", "Sec-Fetch-Site", "cross-site"}, http.StatusForbidden},
		{"json body", title, []string{"Content-Type", "application/json"}, http.StatusUnsupportedMediaType},
		{"text body", title, []string{"Content-Type", "text/plain"}, http.StatusUnsupportedMediaType},
	}
	for _, tc := range cases {
		form := url.Values{}
		for k, v := range tc.form {
			form[k] = v
		}
		if got := f.post("/tasks", form, tc.headers...); got.status != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, got.status, tc.status)
		}
	}
	if after := len(f.events()); after != before {
		t.Fatalf("rejected requests wrote %d events", after-before)
	}
	// Accepted: no Origin (token only), and a "null" Origin with same-origin metadata.
	f.post("/tasks", url.Values{"title": {"a"}}, "Origin", "").redirect(t)
	f.post("/tasks", url.Values{"title": {"b"}}, "Origin", "null", "Sec-Fetch-Site", "same-origin").redirect(t)
	if after := len(f.events()); after != before+2 {
		t.Fatalf("accepted requests wrote %d events", after-before)
	}
	// Unknown write routes and methods never reach the Board.
	if got := f.post("/tasks/1/delete", url.Values{}); got.status != http.StatusNotFound {
		t.Errorf("unknown route status %d", got.status)
	}
	if got := f.do(httptest.NewRequest(http.MethodPut, "/tasks", nil)); got.status != http.StatusMethodNotAllowed {
		t.Errorf("PUT status %d", got.status)
	}
	if got := f.do(httptest.NewRequest(http.MethodPost, "/live", nil)); got.status != http.StatusMethodNotAllowed {
		t.Errorf("POST /live status %d", got.status)
	}
}

func TestHeadersETagAndAssets(t *testing.T) {
	f := newFixture(t)
	page := f.get("/")
	for header, want := range map[string]string{
		"Content-Security-Policy": contentSecurityPolicy,
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "same-origin",
		"Cache-Control":           "no-store",
	} {
		if got := page.header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if strings.Contains(page.body, "<script>") || strings.Contains(page.body, "style=") {
		t.Error("page has inline script or style, which the CSP forbids")
	}

	live := f.get("/live")
	etag := live.header.Get("ETag")
	if etag == "" || !strings.Contains(page.body, `data-etag="`+strings.Trim(etag, `"`)) && !strings.Contains(page.body, `data-etag="&#34;`+strings.Trim(etag, `"`)+`&#34;"`) {
		t.Fatalf("page does not embed the live ETag %s", etag)
	}
	request := httptest.NewRequest(http.MethodGet, "/live", nil)
	request.Header.Set("If-None-Match", etag)
	if got := f.do(request); got.status != http.StatusNotModified || got.body != "" {
		t.Fatalf("unchanged live: status %d", got.status)
	}
	f.create("changed elsewhere")
	if got := f.do(request); got.status != http.StatusOK || got.header.Get("ETag") == etag {
		t.Fatalf("changed live: status %d etag %s", got.status, got.header.Get("ETag"))
	}

	for _, name := range []string{"board.css", "board.js"} {
		asset := f.get("/assets/" + name)
		if asset.status != http.StatusOK || asset.header.Get("ETag") == "" || asset.header.Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: status %d headers %v", name, asset.status, asset.header)
		}
	}
	if got := f.get("/assets/page.html"); got.status != http.StatusNotFound {
		t.Errorf("template served as asset: %d", got.status)
	}
	if got := f.do(httptest.NewRequest(http.MethodHead, "/", nil)); got.status != http.StatusOK || got.body != "" {
		t.Errorf("HEAD: status %d body %d bytes", got.status, len(got.body))
	}
}

func TestBannersComeFromClosedSets(t *testing.T) {
	f := newFixture(t)
	page := f.get("/?notice=%3Cscript%3Ealert(1)%3C%2Fscript%3E&field=%3Cb%3E").body
	if strings.Contains(page, "alert(1)") || strings.Contains(page, "board-banner") {
		t.Fatal("unknown notice rendered")
	}
	page = f.get("/?error=SOMETHING_ELSE&field=%3Cb%3Ex").body
	if !strings.Contains(page, zhCN.ErrorUnknown) || strings.Contains(page, "&lt;b&gt;x") {
		t.Fatal("unknown error code should render only the generic message")
	}
	page = f.get("/?error=INVALID_ARGUMENT&field=title").body
	if !strings.Contains(page, zhCN.Errors[domain.CodeInvalidArgument]+zhCN.FieldPrefix+zhCN.FieldNames["title"]) {
		t.Fatal("known error with field not rendered")
	}
	if !strings.Contains(f.get("/?notice=queued").body, zhCN.Notices["queued"]) {
		t.Fatal("known notice not rendered")
	}
	// The new-task modal is open only on request.
	if !strings.Contains(f.get("/").body, `id="new-task-modal" hidden`) || strings.Contains(f.get("/?new=1").body, `id="new-task-modal" hidden`) {
		t.Fatal("modal visibility")
	}
}

func TestDrawerShowsReadyRankOnlyForReady(t *testing.T) {
	f := newFixture(t)
	// Tasks 1-3 are queued, then moved out of READY; their stored rank is untouched.
	inProgress := f.setState(f.queue(f.create("in progress")), domain.StateInProgress)
	done := f.setState(f.queue(f.create("done")), domain.StateDone)
	blocked := f.setState(f.queue(f.create("blocked")), domain.StateBlocked)
	unqueued := f.create("unqueued")
	ready := f.queue(f.create("ready"))
	if ready.ReadyRank == nil {
		t.Fatal("queued task has no ready rank")
	}

	page := f.get(fmt.Sprintf("/?task=%d", ready.Number))
	if !strings.Contains(page.body, zhCN.ReadyRank) || !strings.Contains(page.body, "data-ready-rank>"+fmt.Sprint(*ready.ReadyRank)+"<") {
		t.Errorf("READY task detail does not show its ready rank %d", *ready.ReadyRank)
	}
	for _, task := range []domain.Task{inProgress, done, blocked, unqueued} {
		page := f.get(fmt.Sprintf("/?task=%d", task.Number))
		if !strings.Contains(page.body, fmt.Sprintf(`data-detail="%d"`, task.Number)) {
			t.Fatalf("task %d detail not rendered", task.Number)
		}
		if strings.Contains(page.body, zhCN.ReadyRank) || strings.Contains(page.body, "data-ready-rank") {
			t.Errorf("task %d detail shows ready rank", task.Number)
		}
	}
}
