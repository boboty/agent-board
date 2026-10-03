// Package e2e drives the real aboard binary with a real browser
// (headless Chrome over the DevTools protocol). It is a separate module so
// the browser driver stays out of the product's dependencies.
//
// Run: cd e2e && go test ./...   (CHROME_PATH selects the browser binary;
// the test skips when no Chrome/Chromium is installed.)
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type board struct {
	t       *testing.T
	bin     string
	project string
	env     []string
	url     string
}

func (b *board) cli(args ...string) string {
	b.t.Helper()
	cmd := exec.Command(b.bin, args...)
	cmd.Dir = b.project
	cmd.Env = append(b.env, "AGENT_BOARD_ACTOR=cli-agent")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			b.t.Fatalf("aboard %v: %v: %s", args, err, exitErr.Stderr)
		}
		b.t.Fatalf("aboard %v: %v", args, err)
	}
	return string(out)
}

type task struct {
	ID          string  `json:"id"`
	Number      int64   `json:"number"`
	Title       string  `json:"title"`
	State       *string `json:"state"`
	StateReason *string `json:"state_reason"`
	Version     int64   `json:"version"`
}

func (b *board) task(ref string) task {
	b.t.Helper()
	var result struct{ Task task }
	if err := json.Unmarshal([]byte(b.cli("task", "get", ref)), &result); err != nil {
		b.t.Fatal(err)
	}
	return result.Task
}

func (b *board) readyNumbers() []int64 {
	b.t.Helper()
	var queue struct{ Tasks []task }
	if err := json.Unmarshal([]byte(b.cli("ready", "list")), &queue); err != nil {
		b.t.Fatal(err)
	}
	var numbers []int64
	for _, t := range queue.Tasks {
		numbers = append(numbers, t.Number)
	}
	return numbers
}

func startBoard(t *testing.T) *board {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	b := &board{t: t, bin: filepath.Join(tmp, "aboard"), project: filepath.Join(tmp, "repo")}
	build := exec.Command("go", "build", "-o", b.bin, "./cmd/aboard")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := filepath.Join(tmp, "home")
	for _, dir := range []string{b.project, home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "HOME=") && !strings.HasPrefix(kv, "USERPROFILE=") && !strings.HasPrefix(kv, "AGENT_BOARD_ACTOR=") {
			b.env = append(b.env, kv)
		}
	}
	b.env = append(b.env, "HOME="+home, "USERPROFILE="+home)
	if out, err := exec.Command("git", "-C", b.project, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	b.cli("init")

	ctx, cancel := context.WithCancel(context.Background())
	server := exec.CommandContext(ctx, b.bin, "web", "--addr", "127.0.0.1:0")
	server.Dir = b.project
	server.Env = b.env // no AGENT_BOARD_ACTOR: web writes are recorded as "web"
	stdout, err := server.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = server.Wait() })
	var started struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(bufio.NewReader(stdout)).Decode(&started); err != nil {
		t.Fatal(err)
	}
	b.url = strings.TrimSuffix(started.URL, "/")
	return b
}

func newBrowser(t *testing.T) context.Context {
	t.Helper()
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.WindowSize(1400, 900))
	if path := os.Getenv("CHROME_PATH"); path != "" {
		options = append(options, chromedp.ExecPath(path))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), options...)
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	ctx, cancelTimeout := context.WithTimeout(ctx, 2*time.Minute)
	t.Cleanup(func() { cancelTimeout(); cancelCtx(); cancelAlloc() })
	if err := chromedp.Run(ctx); err != nil {
		if errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found") {
			t.Skipf("no Chrome available: %v", err)
		}
		t.Fatal(err)
	}
	return ctx
}

func run(t *testing.T, ctx context.Context, step string, actions ...chromedp.Action) {
	t.Helper()
	if err := chromedp.Run(ctx, actions...); err != nil {
		t.Fatalf("%s: %v", step, err)
	}
}

// waitFor polls a JS boolean expression until it is true; navigation in
// between is tolerated.
func waitFor(t *testing.T, ctx context.Context, step, expression string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var ok bool
		err := chromedp.Run(ctx, chromedp.Evaluate(expression, &ok))
		if err == nil && ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: timed out waiting for %s (last error %v)", step, expression, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func evalJSON[T any](t *testing.T, ctx context.Context, expression string) T {
	t.Helper()
	var value T
	run(t, ctx, expression, chromedp.Evaluate(expression, &value, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
	return value
}

// columnsJS reads the rendered board: column state -> task numbers.
const columnsJS = `(() => {
  const out = {};
  document.querySelectorAll('[data-column]').forEach(col => {
    out[col.dataset.column] = [...col.querySelectorAll('[data-task]')].map(e => Number(e.dataset.task));
  });
  out.UNQUEUED = [...document.querySelectorAll('[data-unqueued] [data-task]')].map(e => Number(e.dataset.task));
  return out;
})()`

func columnHas(state string, number int64) string {
	return fmt.Sprintf(`!!document.querySelector('[data-column=%q] [data-task="%d"]')`, state, number)
}

func TestWebBoardInRealBrowser(t *testing.T) {
	b := startBoard(t)
	// Seed through the CLI, as an agent would.
	b.cli("task", "create", "--title", "Ready one")
	b.cli("task", "create", "--title", "Ready two")
	b.cli("task", "create", "--title", "Working")
	b.cli("task", "create", "--title", "Idea not queued")
	b.cli("task", "queue", "1", "--version", "1")
	b.cli("task", "queue", "2", "--version", "1")
	b.cli("task", "queue", "3", "--version", "1")
	b.cli("task", "set-state", "3", "IN_PROGRESS", "--version", "2")

	ctx := newBrowser(t)
	run(t, ctx, "open board", chromedp.Navigate(b.url+"/"), chromedp.WaitVisible(`[data-column="READY"]`))

	// Four columns straight from recorded state; the unqueued task is not READY.
	layout := evalJSON[map[string][]int64](t, ctx, columnsJS)
	want := map[string][]int64{"READY": {1, 2}, "IN_PROGRESS": {3}, "DONE": {}, "BLOCKED": {}, "UNQUEUED": {4}}
	for name, numbers := range want {
		if !slices.Equal(layout[name], numbers) {
			t.Fatalf("%s = %v, want %v (layout %v)", name, layout[name], numbers, layout)
		}
	}

	// Create through the modal (opened by script, no navigation).
	run(t, ctx, "open modal",
		chromedp.Click(`#new-task-btn`),
		chromedp.WaitVisible(`#new-task-modal input[name=title]`),
		chromedp.SendKeys(`#new-task-modal input[name=title]`, "Web Board E2E"),
		chromedp.SendKeys(`#new-task-modal textarea[name=description]`, "created in a real browser"),
		chromedp.Click(`#new-task-modal button[type=submit]`),
	)
	waitFor(t, ctx, "created drawer", `!!document.querySelector('[data-detail="5"]')`)
	if created := b.task("5"); created.State != nil || created.Title != "Web Board E2E" {
		t.Fatalf("created %+v", created)
	}
	if layout := evalJSON[map[string][]int64](t, ctx, columnsJS); !slices.Contains(layout["UNQUEUED"], 5) || slices.Contains(layout["READY"], 5) {
		t.Fatalf("new task must be unqueued, not READY: %v", layout)
	}

	// Queue from the drawer.
	run(t, ctx, "queue", chromedp.Click(`#drawer button[data-action=queue]`))
	waitFor(t, ctx, "queued", columnHas("READY", 5))
	if got := b.readyNumbers(); !slices.Equal(got, []int64{1, 2, 5}) {
		t.Fatalf("READY after queue %v", got)
	}

	// Reorder: move #5 up twice.
	run(t, ctx, "close drawer", chromedp.Navigate(b.url+"/"), chromedp.WaitVisible(`[data-column="READY"]`))
	run(t, ctx, "move up", chromedp.Click(`[data-column="READY"] [data-task="5"] button[data-action=move-up]`))
	waitFor(t, ctx, "moved once", `[...document.querySelectorAll('[data-column="READY"] [data-task]')].map(e => e.dataset.task).join() === "1,5,2"`)
	run(t, ctx, "move up again", chromedp.Click(`[data-column="READY"] [data-task="5"] button[data-action=move-up]`))
	waitFor(t, ctx, "moved twice", `[...document.querySelectorAll('[data-column="READY"] [data-task]')].map(e => e.dataset.task).join() === "5,1,2"`)
	if got := b.readyNumbers(); !slices.Equal(got, []int64{5, 1, 2}) {
		t.Fatalf("list_ready after reorder %v", got)
	}

	// Set state BLOCKED with a reason from the drawer.
	run(t, ctx, "set state",
		chromedp.Navigate(b.url+"/?task=5"),
		chromedp.WaitVisible(`#drawer`),
		chromedp.Click(`#action-state summary`),
		chromedp.SetValue(`#action-state select[name=state]`, "BLOCKED"),
		chromedp.SendKeys(`#action-state textarea[name=reason]`, "needs a product decision"),
		chromedp.Click(`#action-state button[data-action=set-state]`),
	)
	waitFor(t, ctx, "blocked", columnHas("BLOCKED", 5))
	blocked := b.task("5")
	if blocked.State == nil || *blocked.State != "BLOCKED" || blocked.StateReason == nil || *blocked.StateReason != "needs a product decision" {
		t.Fatalf("after set state %+v", blocked)
	}

	// Record a verification fact: it is listed, and the state does not move.
	run(t, ctx, "record fact",
		chromedp.Click(`#action-fact summary`),
		chromedp.SetValue(`#action-fact select[name=kind]`, "verification"),
		chromedp.SendKeys(`#action-fact textarea[name=body]`, "PASS: verified in browser"),
		chromedp.SendKeys(`#action-fact textarea[name=data]`, `{"verdict":"PASS"}`),
		chromedp.Click(`#action-fact button[data-action=record-fact]`),
	)
	waitFor(t, ctx, "fact listed", `[...document.querySelectorAll('[data-facts] .fact')].some(f => f.textContent.includes('PASS: verified in browser'))`)
	if after := b.task("5"); *after.State != "BLOCKED" || after.Version != blocked.Version {
		t.Fatalf("fact changed the task: %+v", after)
	}
	waitFor(t, ctx, "still blocked", columnHas("BLOCKED", 5)+` && !`+columnHas("DONE", 5))
	longBody := strings.Repeat("Long delivery evidence stays in the history. ", 8) + "e2e-full-body-tail"
	b.cli("fact", "record", "5", "--kind", "delivery", "--body", longBody, "--data", `{"commit":"e2e123"}`)
	run(t, ctx, "open detail summaries", chromedp.Navigate(b.url+"/?task=5"), chromedp.WaitVisible(`#drawer [data-key-facts]`))
	summaries := evalJSON[[]string](t, ctx, `[
  document.querySelector('[data-key-fact-kind="delivery"]')?.textContent || '',
  document.querySelector('[data-facts]')?.textContent || ''
]`)
	if !strings.Contains(summaries[0], "e2e123") || strings.Contains(summaries[0], "e2e-full-body-tail") {
		t.Fatalf("detail delivery summary was not compact: %q", summaries[0])
	}
	if !strings.Contains(summaries[1], "e2e-full-body-tail") {
		t.Fatalf("full delivery body missing from fact history: %q", summaries[1])
	}

	// Live refresh: an agent changes state through the CLI; the open page
	// follows without a reload.
	run(t, ctx, "board", chromedp.Navigate(b.url+"/"), chromedp.WaitVisible(`[data-column="READY"]`))
	run(t, ctx, "mark page", chromedp.Evaluate(`window.__noReload = true`, nil))
	b.cli("task", "set-state", "3", "DONE", "--version", "3")
	waitFor(t, ctx, "live refresh", columnHas("DONE", 3)+` && !`+columnHas("IN_PROGRESS", 3)+` && window.__noReload === true`)

	// Browser security: a script without the token is refused, and a form
	// posted from another origin is refused even with the real token.
	status := evalJSON[float64](t, ctx, `fetch('/tasks', {method: 'POST', body: new URLSearchParams({title: 'no token'})}).then(r => r.status)`)
	if status != http.StatusForbidden {
		t.Fatalf("tokenless post: status %v", status)
	}
	token := evalJSON[string](t, ctx, `document.querySelector('input[name=csrf_token]').value`)
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<form id="f" method="post" action="%s/tasks"><input name="csrf_token" value="%s"><input name="title" value="forged"></form><script>document.getElementById('f').submit()</script>`, b.url, token)
	}))
	defer attacker.Close()
	run(t, ctx, "cross-origin post", chromedp.Navigate(attacker.URL))
	waitFor(t, ctx, "cross-origin response", fmt.Sprintf(`location.href === %q`, b.url+"/tasks"))
	if body := evalJSON[string](t, ctx, `document.body.innerText`); !strings.Contains(body, "Forbidden") {
		t.Fatalf("cross-origin post was not refused: %q", body)
	}
	if out := b.cli("task", "list"); strings.Contains(out, "forged") || strings.Contains(out, "no token") {
		t.Fatal("a refused request created a task")
	}

	// Every web write is in the audit log under the web actor.
	var events struct {
		Events []struct {
			Type  string `json:"type"`
			Actor string `json:"actor"`
		}
	}
	if err := json.Unmarshal([]byte(b.cli("events", "--limit", "1000")), &events); err != nil {
		t.Fatal(err)
	}
	var webTypes []string
	for _, event := range events.Events {
		if event.Actor == "web" {
			webTypes = append(webTypes, event.Type)
		}
	}
	wantTypes := []string{"task_created", "task_queued", "ready_reordered", "ready_reordered", "task_state_set", "fact_recorded"}
	if !slices.Equal(webTypes, wantTypes) {
		t.Fatalf("web audit events %v, want %v", webTypes, wantTypes)
	}
}

func TestCompletedHistoryInRealBrowser(t *testing.T) {
	b := startBoard(t)
	ctx := newBrowser(t)
	run(t, ctx, "open empty history", chromedp.Navigate(b.url+"/completed/"), chromedp.WaitVisible(`.completed-page`))
	if body := evalJSON[string](t, ctx, `document.body.innerText`); !strings.Contains(body, "暂无已完成任务") || !strings.Contains(body, "返回首页") {
		t.Fatalf("empty completed history or home link missing: %q", body)
	}

	// Create tasks in number order but complete them in that same order, so
	// newest-first display is the reverse of task number/creation order.
	for i := 1; i <= 21; i++ {
		b.cli("task", "create", "--title", fmt.Sprintf("History task %02d", i))
		b.cli("task", "queue", fmt.Sprint(i), "--version", "1")
		b.cli("task", "set-state", fmt.Sprint(i), "DONE", "--version", "2")
	}

	run(t, ctx, "open home", chromedp.Navigate(b.url+"/"), chromedp.WaitVisible(`[data-column="DONE"]`))
	homeDone := evalJSON[[]int64](t, ctx, `[
  ...document.querySelectorAll('[data-column="DONE"] [data-task]')
].map(e => Number(e.dataset.task))`)
	if !slices.Equal(homeDone, []int64{21, 20, 19, 18, 17, 16, 15, 14}) {
		t.Fatalf("home DONE cards %v, want latest eight", homeDone)
	}
	if body := evalJSON[string](t, ctx, `document.querySelector('[data-column="DONE"]').innerText`); !strings.Contains(body, "当前已完成: 21") || !strings.Contains(body, "查看全部已完成") {
		t.Fatalf("home total/history entry missing: %q", body)
	}
	run(t, ctx, "open completed history", chromedp.Click(`[data-column="DONE"] a[href="/completed/"]`), chromedp.WaitVisible(`.completed-list`))
	first := evalJSON[map[string]any](t, ctx, `({
  page: Number(document.querySelector('[data-completed-page]').dataset.completedPage),
  total: document.querySelector('.completed-heading').innerText,
  count: document.querySelectorAll('.completed-list [data-task]').length,
  numbers: [...document.querySelectorAll('.completed-list [data-task]')].map(e => Number(e.dataset.task))
})`)
	if first["page"] != float64(1) || first["count"] != float64(20) || !strings.Contains(first["total"].(string), "21") {
		t.Fatalf("first history page metadata: %#v", first)
	}

	run(t, ctx, "go to last history page", chromedp.Click(`.completed-pagination a[href="/completed/?page=2"]`), chromedp.WaitVisible(`[data-completed-page="2"]`))
	lastNumbers := evalJSON[[]int64](t, ctx, `[...document.querySelectorAll('.completed-list [data-task]')].map(e => Number(e.dataset.task))`)
	if !slices.Equal(lastNumbers, []int64{1}) || evalJSON[bool](t, ctx, `!!document.querySelector('.completed-pagination a[href*="page=3"]')`) {
		t.Fatalf("last page tasks/navigation: %v", lastNumbers)
	}
	run(t, ctx, "return to first history page", chromedp.Click(`.completed-pagination a[href="/completed/?page=1"]`), chromedp.WaitVisible(`[data-completed-page="1"]`))
	if got := evalJSON[int](t, ctx, `document.querySelectorAll('.completed-list [data-task]').length`); got != 20 {
		t.Fatalf("first history page contains %d tasks, want 20", got)
	}
	run(t, ctx, "return to last history page", chromedp.Click(`.completed-pagination a[href="/completed/?page=2"]`), chromedp.WaitVisible(`[data-completed-page="2"]`))
	run(t, ctx, "open task detail from history", chromedp.Click(`.completed-list a[href="/?task=1"]`), chromedp.WaitVisible(`#drawer[data-detail="1"]`))
	run(t, ctx, "return to home from detail", chromedp.Click(`#drawer-close`), chromedp.WaitVisible(`[data-column="READY"]`))
	if !strings.Contains(evalJSON[string](t, ctx, `document.body.innerText`), "AI 研发任务看板") {
		t.Fatal("task detail did not return to home")
	}
}
