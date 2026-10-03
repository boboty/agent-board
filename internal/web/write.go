package web

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ops"
)

// The write gate below (synchronizer token, same-origin check, form content
// type, body limit, POST/redirect/GET with closed-set banner codes) is ported
// from the served board of boboty/agent-board-rhizome-poc (branch
// agent-board-v0.1, board_write_http.go). The routes and the operations
// behind them are Agent Board's own: each one calls exactly one ops method.

// writeBodyLimit bounds one form post; the forms are a few text fields.
const writeBodyLimit = 64 << 10

// writeAction runs one Board operation from a parsed form and returns the
// query of the page to show on success.
type writeAction func(r *http.Request) (url.Values, error)

// write wraps an action with the browser security checks and the redirect.
// A request that fails a security check gets a plain 403/415 and never
// reaches the Board. A Board error redirects back with the Board's own error
// code, so the page reloads current data with an explanation.
func (h *handler) write(action writeAction) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		mediaType := strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])
		if !strings.EqualFold(mediaType, "application/x-www-form-urlencoded") {
			http.Error(w, "application/x-www-form-urlencoded required", http.StatusUnsupportedMediaType)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, writeBodyLimit)
		if err := r.ParseForm(); err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		token := r.PostForm.Get("csrf_token")
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(h.csrf)) != 1 {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		next, err := action(r)
		if err != nil {
			next = returnTo(r)
			described := ops.DescribeError(err)
			next.Set("error", described.Code)
			if len(described.Details) > 0 && described.Details[0].Field != "" {
				next.Set("field", described.Details[0].Field)
			}
		}
		target := "/"
		if len(next) > 0 {
			target += "?" + next.Encode()
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

// sameOrigin rejects requests the browser labels cross-site, and an Origin
// that does not match the Host. A missing Origin is allowed: the token is the
// authoritative defense. The serving wrapper performs the same Origin check
// for every route; this keeps the write handler safe on its own.
func sameOrigin(r *http.Request) bool {
	site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	if site == "cross-site" || site == "same-site" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	switch origin {
	case "":
		return true
	case "null":
		return site == "same-origin" || site == "none"
	}
	return origin == "http://"+strings.TrimSpace(r.Host)
}

// returnTo is where a failed write sends the browser: the task drawer or the
// new-task modal it came from, else the board. Only a task number or the
// modal flag is accepted, so the redirect cannot leave the board.
func returnTo(r *http.Request) url.Values {
	if number := r.PostForm.Get("return_task"); number != "" {
		if _, err := strconv.ParseUint(number, 10, 63); err == nil {
			return url.Values{"task": {number}}
		}
	}
	if r.PostForm.Get("return_new") == "1" {
		return url.Values{"new": {"1"}}
	}
	return url.Values{}
}

func success(notice string, task *domain.Task) url.Values {
	values := url.Values{"notice": {notice}}
	if task != nil {
		values.Set("task", fmt.Sprint(task.Number))
	}
	return values
}

func writeArgs(r *http.Request) ops.Write {
	return ops.Write{IdempotencyKey: r.PostForm.Get("idempotency_key")}
}

// text reads a form field, normalizing the CRLF line breaks browsers submit
// for textareas so unchanged text compares equal to what was stored.
func text(r *http.Request, field string) string {
	return strings.ReplaceAll(r.PostForm.Get(field), "\r\n", "\n")
}

// optionalText is nil for an empty field.
func optionalText(r *http.Request, field string) *string {
	value := text(r, field)
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func expectedVersion(r *http.Request) (int64, error) {
	version, err := strconv.ParseInt(r.PostForm.Get("expected_version"), 10, 64)
	if err != nil {
		return 0, domain.Invalid("expected_version", "must be an integer")
	}
	return version, nil
}

func (h *handler) createTask(r *http.Request) (url.Values, error) {
	result, err := h.ops.CreateTask(r.Context(), ops.CreateTaskArgs{
		Write:              writeArgs(r),
		Title:              text(r, "title"),
		Description:        text(r, "description"),
		AcceptanceCriteria: text(r, "acceptance_criteria"),
	})
	if err != nil {
		return nil, err
	}
	return success("created", &result.Task), nil
}

func (h *handler) updateTask(r *http.Request) (url.Values, error) {
	version, err := expectedVersion(r)
	if err != nil {
		return nil, err
	}
	title, description, criteria := text(r, "title"), text(r, "description"), text(r, "acceptance_criteria")
	result, err := h.ops.UpdateTask(r.Context(), ops.UpdateTaskArgs{
		Write:              writeArgs(r),
		Task:               r.PathValue("ref"),
		ExpectedVersion:    version,
		Title:              &title,
		Description:        &description,
		AcceptanceCriteria: &criteria,
	})
	if err != nil {
		return nil, err
	}
	return success("updated", &result.Task), nil
}

func (h *handler) queueTask(r *http.Request) (url.Values, error) {
	version, err := expectedVersion(r)
	if err != nil {
		return nil, err
	}
	result, err := h.ops.QueueTask(r.Context(), ops.QueueTaskArgs{
		Write:           writeArgs(r),
		Task:            r.PathValue("ref"),
		ExpectedVersion: version,
	})
	if err != nil {
		return nil, err
	}
	return withReturn(r, success("queued", &result.Task)), nil
}

func (h *handler) setTaskState(r *http.Request) (url.Values, error) {
	version, err := expectedVersion(r)
	if err != nil {
		return nil, err
	}
	result, err := h.ops.SetTaskState(r.Context(), ops.SetTaskStateArgs{
		Write:           writeArgs(r),
		Task:            r.PathValue("ref"),
		ExpectedVersion: version,
		State:           domain.State(r.PostForm.Get("state")),
		Reason:          optionalText(r, "reason"),
	})
	if err != nil {
		return nil, err
	}
	return success("state", &result.Task), nil
}

func (h *handler) recordFact(r *http.Request) (url.Values, error) {
	args := ops.RecordFactArgs{
		Write: writeArgs(r),
		Task:  r.PathValue("ref"),
		Kind:  domain.FactKind(r.PostForm.Get("kind")),
		Body:  text(r, "body"),
		Provenance: &domain.FactProvenance{
			Role: r.PostForm.Get("role"), Session: r.PostForm.Get("session"),
			Harness: r.PostForm.Get("harness"), Model: r.PostForm.Get("model"),
		},
	}
	if data := optionalText(r, "data"); data != nil {
		args.Data = []byte(*data)
	}
	if _, err := h.ops.RecordFact(r.Context(), args); err != nil {
		return nil, err
	}
	return url.Values{"notice": {"fact"}, "task": {r.PathValue("ref")}}, nil
}

func (h *handler) reorderReady(r *http.Request) (url.Values, error) {
	version, err := strconv.ParseInt(r.PostForm.Get("expected_version"), 10, 64)
	if err != nil {
		return nil, domain.Invalid("expected_version", "must be an integer")
	}
	if _, err := h.ops.ReorderReady(r.Context(), ops.ReorderReadyArgs{
		Write:           writeArgs(r),
		ExpectedVersion: version,
		Tasks:           r.PostForm["tasks"],
	}); err != nil {
		return nil, err
	}
	return url.Values{"notice": {"reordered"}}, nil
}

// withReturn keeps the drawer closed when the action came from the board
// itself (no return_task), such as queueing from the unqueued list.
func withReturn(r *http.Request, values url.Values) url.Values {
	if r.PostForm.Get("return_task") == "" {
		values.Del("task")
	}
	return values
}
