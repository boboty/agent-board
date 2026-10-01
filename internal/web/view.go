package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/boboty/agent-board/internal/board"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ops"
)

// boardView is the live region of the page: the four state columns, the
// unqueued list, and the open task's detail. It is a presentation of Board
// reads only; nothing here derives or changes state.
type boardView struct {
	T            *UIStrings
	CSRF         string
	Columns      []column
	Unqueued     []domain.Task
	ReadyVersion int64
	Detail       *detailView
	States       []domain.State
	FactKinds    []domain.FactKind
}

type column struct {
	State domain.State
	Cards []card
}

// card is one task in a column. For READY cards, Up and Down hold the full
// READY order after moving the task one place, ready to submit to
// reorder_ready; they are nil where no move is possible and in other columns.
type card struct {
	domain.Task
	Up, Down []string
}

type detailView struct {
	Task            domain.Task
	Latest          []domain.TaskFact
	Facts           []domain.TaskFact
	Events          []domain.TaskEvent
	EventsTruncated bool
}

// groupByState places every task by its recorded state, and only by it: one
// column per domain state, in domain order, and tasks with no state (not yet
// queued) in a separate list that is not a column. READY tasks are shown in
// their recorded READY rank order.
func groupByState(tasks []domain.Task) ([]column, []domain.Task) {
	columns := make([]column, len(domain.States))
	index := make(map[domain.State]int, len(domain.States))
	for i, state := range domain.States {
		columns[i] = column{State: state, Cards: []card{}}
		index[state] = i
	}
	unqueued := []domain.Task{}
	for _, task := range tasks {
		if task.State == nil {
			unqueued = append(unqueued, task)
			continue
		}
		i := index[*task.State]
		columns[i].Cards = append(columns[i].Cards, card{Task: task})
	}
	ready := columns[index[domain.StateReady]].Cards
	slices.SortStableFunc(ready, func(a, b card) int {
		return compareRank(a.ReadyRank, b.ReadyRank)
	})
	setReadyMoves(ready)
	return columns, unqueued
}

func compareRank(a, b *int64) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	case *a < *b:
		return -1
	case *a > *b:
		return 1
	}
	return 0
}

// setReadyMoves precomputes each READY card's swap with its neighbor. The
// forms submit the whole order with the READY version that was read before
// it, so the Board's version check rejects a reorder based on a stale view.
func setReadyMoves(cards []card) {
	ids := make([]string, len(cards))
	for i, c := range cards {
		ids[i] = c.ID
	}
	swapped := func(i, j int) []string {
		order := slices.Clone(ids)
		order[i], order[j] = order[j], order[i]
		return order
	}
	for i := range cards {
		if i > 0 {
			cards[i].Up = swapped(i, i-1)
		}
		if i < len(cards)-1 {
			cards[i].Down = swapped(i, i+1)
		}
	}
}

// loadBoard reads the board through ops. The READY version is read before the
// task list, so it is never newer than the order the page shows.
func loadBoard(ctx context.Context, service *ops.Service, taskRef string) (boardView, error) {
	ready, err := service.ListReady(ctx, ops.ListReadyArgs{})
	if err != nil {
		return boardView{}, err
	}
	list, err := service.ListTasks(ctx, ops.ListTasksArgs{})
	if err != nil {
		return boardView{}, err
	}
	view := boardView{T: &zhCN, ReadyVersion: ready.Version, States: domain.States, FactKinds: domain.FactKinds}
	view.Columns, view.Unqueued = groupByState(list.Tasks)
	if taskRef == "" {
		return view, nil
	}
	detail, err := loadDetail(ctx, service, taskRef)
	if err != nil {
		return boardView{}, err
	}
	view.Detail = &detail
	return view, nil
}

func loadDetail(ctx context.Context, service *ops.Service, ref string) (detailView, error) {
	task, err := service.GetTask(ctx, ops.GetTaskArgs{Task: ref})
	if err != nil {
		return detailView{}, err
	}
	facts, err := service.ListFacts(ctx, ops.ListFactsArgs{Task: task.Task.ID})
	if err != nil {
		return detailView{}, err
	}
	events, err := service.ListEvents(ctx, ops.ListEventsArgs{Task: task.Task.ID, Limit: board.MaxEventLimit})
	if err != nil {
		return detailView{}, err
	}
	return detailView{
		Task:            task.Task,
		Latest:          latestByKind(facts.Facts),
		Facts:           facts.Facts,
		Events:          events.Events,
		EventsTruncated: len(events.Events) == board.MaxEventLimit,
	}, nil
}

// latestByKind picks the most recent execution, delivery, verification, and
// handoff fact, in that order, for the detail summary. It only selects facts
// to display; it does not interpret them.
func latestByKind(facts []domain.TaskFact) []domain.TaskFact {
	var latest []domain.TaskFact
	for _, kind := range domain.FactKinds {
		if kind == domain.FactNote {
			continue
		}
		for i := len(facts) - 1; i >= 0; i-- {
			if facts[i].Kind == kind {
				latest = append(latest, facts[i])
				break
			}
		}
	}
	return latest
}

// factSummary gives the detail header a compact, presentation-only view of a
// fact. It prefers a few known identity fields and falls back to the opening
// paragraph of the body; the full fact remains available in the history below.
func factSummary(fact domain.TaskFact) string {
	keys := map[domain.FactKind][]string{
		domain.FactExecution:    {"role", "model", "harness"},
		domain.FactDelivery:     {"accepted_commit", "commit", "files", "file_count"},
		domain.FactVerification: {"verdict", "round"},
		domain.FactHandoff:      {"worktree", "branch"},
	}
	var parts []string
	var data map[string]json.RawMessage
	if len(fact.Data) > 0 && json.Unmarshal(fact.Data, &data) == nil {
		for _, key := range keys[fact.Kind] {
			if value, ok := summaryDataValue(data[key]); ok {
				parts = append(parts, key+" "+value)
			}
		}
	}
	body := summaryBody(fact.Body)
	if body != "" {
		parts = append(parts, body)
	}
	return strings.Join(parts, " · ")
}

func summaryDataValue(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	switch value := value.(type) {
	case string:
		value = strings.Join(strings.Fields(value), " ")
		return truncateSummary(value, 64), value != ""
	case float64, bool:
		return fmt.Sprint(value), true
	default:
		return "", false
	}
}

func summaryBody(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	// A blank line ends the opening paragraph. Collapse remaining whitespace
	// so the summary stays compact even when the fact is formatted as prose.
	if end := strings.Index(body, "\n\n"); end >= 0 {
		body = body[:end]
	}
	return truncateSummary(strings.Join(strings.Fields(body), " "), 180)
}

func truncateSummary(value string, maxRunes int) string {
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
}

// prettyJSON indents a stored JSON object for display; invalid input is
// shown as is.
func prettyJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}
