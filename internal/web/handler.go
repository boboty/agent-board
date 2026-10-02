// Package web is the Web Board: a browser surface for people to observe and
// operate the Board. It is an adapter like the CLI and MCP server: every read
// and write goes through ops, and the Board performs all validation, version
// checks, idempotency, transactions, and audit. The page shows tasks in four
// columns taken directly from each task's recorded state; unqueued tasks
// (no state) are listed separately. Nothing here infers or changes state.
package web

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"mime"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ops"
)

//go:embed assets
var assetFS embed.FS

// contentSecurityPolicy allows only same-origin scripts, styles, form
// targets, and fetches; no inline script or style.
const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// Info describes the project and actor shown on the Web Board.
type Info struct {
	ProjectName string
	ProjectID   string
	Actor       string
}

type handler struct {
	ops    *ops.Service
	info   Info
	csrf   string
	tmpl   *template.Template
	assets map[string]asset
}

type asset struct {
	body        []byte
	contentType string
	etag        string
}

// NewHandler serves the Web Board over service. Mutations are recorded with
// service's default actor.
func NewHandler(service *ops.Service, info Info) (http.Handler, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	tmpl, err := template.New("").Funcs(templateFuncs).ParseFS(assetFS, "assets/*.html")
	if err != nil {
		return nil, err
	}
	h := &handler{
		ops:    service,
		info:   info,
		csrf:   base64.RawURLEncoding.EncodeToString(token),
		tmpl:   tmpl,
		assets: map[string]asset{},
	}
	for _, name := range []string{"board.css", "board.js"} {
		body, err := assetFS.ReadFile("assets/" + name)
		if err != nil {
			return nil, err
		}
		h.assets[name] = asset{body: body, contentType: mime.TypeByExtension(path.Ext(name)), etag: etagOf(body)}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.servePage)
	mux.HandleFunc("GET /completed/{$}", h.serveCompleted)
	mux.HandleFunc("GET /live", h.serveLive)
	mux.HandleFunc("GET /assets/{name}", h.serveAsset)
	mux.HandleFunc("POST /tasks", h.write(h.createTask))
	mux.HandleFunc("POST /tasks/{ref}", h.write(h.updateTask))
	mux.HandleFunc("POST /tasks/{ref}/queue", h.write(h.queueTask))
	mux.HandleFunc("POST /tasks/{ref}/state", h.write(h.setTaskState))
	mux.HandleFunc("POST /tasks/{ref}/facts", h.write(h.recordFact))
	mux.HandleFunc("POST /ready/order", h.write(h.reorderReady))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		// same-origin keeps a real Origin header on same-origin form posts
		// (no-referrer would make browsers send "Origin: null").
		header.Set("Referrer-Policy", "same-origin")
		header.Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	}), nil
}

// pageView is the whole page; Live is the pre-rendered live region.
type pageView struct {
	T         *UIStrings
	Info      Info
	CSRF      string
	Banner    *banner
	NewOpen   bool
	Live      template.HTML
	ETag      string
	LiveQuery string
	Completed *completedPageView
}

type banner struct {
	Text  string
	Error bool
}

func (h *handler) servePage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	live, liveQuery, status, err := h.renderLive(r, query.Get("task"))
	if err != nil {
		h.serverError(w, err)
		return
	}
	view := pageView{
		T:         &zhCN,
		Info:      h.info,
		CSRF:      h.csrf,
		Banner:    pageBanner(query),
		NewOpen:   query.Get("new") == "1",
		Live:      template.HTML(live),
		ETag:      etagOf(live),
		LiveQuery: liveQuery,
	}
	if status == http.StatusNotFound {
		view.Banner = &banner{Text: zhCN.Errors[domain.CodeTaskNotFound], Error: true}
	}
	var page bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&page, "page", view); err != nil {
		h.serverError(w, err)
		return
	}
	writeBody(w, r, status, "text/html; charset=utf-8", page.Bytes())
}

func (h *handler) serveCompleted(w http.ResponseWriter, r *http.Request) {
	tasks, err := loadCompleted(r.Context(), h.ops)
	if err != nil {
		h.serverError(w, err)
		return
	}
	page := parsePage(r.URL.Query().Get("page"))
	pageCount := max(1, (len(tasks)+completedPageSize-1)/completedPageSize)
	if page > pageCount {
		page = pageCount
	}
	start := (page - 1) * completedPageSize
	end := min(start+completedPageSize, len(tasks))
	view := pageView{
		T:         &zhCN,
		Info:      h.info,
		Banner:    pageBanner(r.URL.Query()),
		Completed: &completedPageView{Tasks: tasks[start:end], Total: len(tasks), Page: page, PageCount: pageCount},
	}
	var pageHTML bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&pageHTML, "page", view); err != nil {
		h.serverError(w, err)
		return
	}
	writeBody(w, r, http.StatusOK, "text/html; charset=utf-8", pageHTML.Bytes())
}

func parsePage(raw string) int {
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 1
	}
	return page
}

// serveLive returns only the live region, with an ETag of its content, so the
// page can poll for changes made by any process and swap them in.
func (h *handler) serveLive(w http.ResponseWriter, r *http.Request) {
	live, _, status, err := h.renderLive(r, r.URL.Query().Get("task"))
	if err != nil {
		h.serverError(w, err)
		return
	}
	etag := etagOf(live)
	w.Header().Set("ETag", etag)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeBody(w, r, status, "text/html; charset=utf-8", live)
}

// renderLive renders the board and, when taskRef names a task, its detail.
// An unknown or malformed reference renders the board alone with status 404.
func (h *handler) renderLive(r *http.Request, taskRef string) ([]byte, string, int, error) {
	status := http.StatusOK
	view, err := loadBoard(r.Context(), h.ops, taskRef)
	if taskRef != "" && (domain.IsCode(err, domain.CodeTaskNotFound) || domain.IsCode(err, domain.CodeInvalidArgument)) {
		status, taskRef = http.StatusNotFound, ""
		view, err = loadBoard(r.Context(), h.ops, "")
	}
	if err != nil {
		return nil, "", 0, err
	}
	view.CSRF = h.csrf
	var out bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&out, "live", view); err != nil {
		return nil, "", 0, err
	}
	liveQuery := ""
	if view.Detail != nil {
		liveQuery = url.Values{"task": {fmt.Sprint(view.Detail.Task.Number)}}.Encode()
	}
	return out.Bytes(), liveQuery, status, nil
}

func (h *handler) serveAsset(w http.ResponseWriter, r *http.Request) {
	a, ok := h.assets[r.PathValue("name")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", a.etag)
	if etagMatches(r.Header.Get("If-None-Match"), a.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeBody(w, r, http.StatusOK, a.contentType, a.body)
}

func (h *handler) serverError(w http.ResponseWriter, err error) {
	described := ops.DescribeError(err)
	http.Error(w, described.Code+": "+described.Message, http.StatusInternalServerError)
}

// pageBanner renders a notice or error only from the closed sets in
// UIStrings, so a crafted query string cannot inject text into the page.
func pageBanner(query url.Values) *banner {
	if code := query.Get("error"); code != "" {
		text, ok := zhCN.Errors[code]
		if !ok {
			text = zhCN.ErrorUnknown
		}
		if field, ok := zhCN.FieldNames[query.Get("field")]; ok {
			text += zhCN.FieldPrefix + field + zhCN.FieldSuffix
		}
		return &banner{Text: text, Error: true}
	}
	if text, ok := zhCN.Notices[query.Get("notice")]; ok {
		return &banner{Text: text}
	}
	return nil
}

func writeBody(w http.ResponseWriter, r *http.Request, status int, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func etagOf(body []byte) string {
	return fmt.Sprintf(`"%x"`, sha256.Sum256(body))
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == etag || candidate == "*" {
			return true
		}
	}
	return false
}

// Sub-template arguments.
type (
	moveArgs struct {
		Board     boardView
		Order     []string
		Label     string
		Direction string
	}
	detailArgs struct {
		Board  boardView
		Detail *detailView
	}
	factArgs struct {
		T    *UIStrings
		Fact domain.TaskFact
	}
	completedArgs struct {
		T    *UIStrings
		Page *completedPageView
	}
)

var templateFuncs = template.FuncMap{
	"stateLabel": func(state domain.State) string { return zhCN.Columns[state] },
	"stateCode":  func(state domain.State) string { return strings.ReplaceAll(string(state), "_", " ") },
	"stateText": func(state *domain.State) string {
		if state == nil {
			return zhCN.NotQueued
		}
		return zhCN.Columns[*state] + " " + strings.ReplaceAll(string(*state), "_", " ")
	},
	"stateClass": func(state *domain.State) string {
		if state == nil {
			return "unqueued"
		}
		return strings.ToLower(strings.ReplaceAll(string(*state), "_", "-"))
	},
	"colClass": func(state domain.State) string {
		return strings.ToLower(strings.ReplaceAll(string(state), "_", "-"))
	},
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	"completedArgs": func(t *UIStrings, page *completedPageView) completedArgs {
		return completedArgs{T: t, Page: page}
	},
	"completedTime": func(t *UIStrings, at *time.Time) string {
		if at == nil {
			return t.CompletedTimeMissing
		}
		return at.Local().Format("2006-01-02 15:04:05")
	},
	"moveArgs": func(board boardView, order []string, label, direction string) moveArgs {
		return moveArgs{Board: board, Order: order, Label: label, Direction: direction}
	},
	"detailArgs": func(board boardView, detail *detailView) detailArgs {
		return detailArgs{Board: board, Detail: detail}
	},
	"factArgs":    func(t *UIStrings, fact domain.TaskFact) factArgs { return factArgs{T: t, Fact: fact} },
	"factSummary": factSummary,
	"isState":     func(state *domain.State, want domain.State) bool { return state != nil && *state == want },
	"kindLabel": func(kind domain.FactKind) string {
		if label, ok := zhCN.FactKinds[kind]; ok {
			return label
		}
		return string(kind)
	},
	"eventLabel": func(eventType domain.EventType) string {
		if label, ok := zhCN.EventTypes[eventType]; ok {
			return label
		}
		return string(eventType)
	},
	"time": func(t time.Time) string { return t.Local().Format("2006-01-02 15:04:05") },
	"timePtr": func(t *time.Time) string {
		if t == nil {
			return "-"
		}
		return t.Local().Format("2006-01-02 15:04:05")
	},
	"int64Ptr": func(v *int64) string {
		if v == nil {
			return "-"
		}
		return fmt.Sprint(*v)
	},
	"pretty":  prettyJSON,
	"compact": func(raw []byte) string { return string(raw) },
	"newestFirst": func(facts []domain.TaskFact) []domain.TaskFact {
		reversed := slices.Clone(facts)
		slices.Reverse(reversed)
		return reversed
	},
}
