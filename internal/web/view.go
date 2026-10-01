package web

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"

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

// prettyJSON indents a stored JSON object for display; invalid input is
// shown as is.
func prettyJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}
