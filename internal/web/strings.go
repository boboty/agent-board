package web

import "github.com/boboty/agent-board/internal/domain"

// UIStrings holds every user-visible string the Web Board renders. Templates,
// scripts, and Go code in this package take text only from here; the i18n
// test keeps CJK text out of every other file. The default locale is zh-CN.
//
// The structure is derived from the POC's BoardUIStrings (one struct, one
// value per locale) and modified for Agent Board; the content is Agent Board's own.
type UIStrings struct {
	ProductName   string
	BoardSubtitle string
	NewTask       string
	ProjectLabel  string
	ActorLabel    string

	Columns              map[domain.State]string
	NoCards              string
	DoneTotalLabel       string
	ViewAllCompleted     string
	NoCompleted          string
	CompletedTitle       string
	CompletedTotalLabel  string
	CompletedAt          string
	CompletedTimeMissing string
	BackHome             string
	Pagination           string
	FirstPage            string
	PreviousPage         string
	NextPage             string
	LastPage             string
	PageLabel            string

	Unqueued     string
	UnqueuedHint string
	NoUnqueued   string
	Queue        string
	MoveUp       string
	MoveDown     string

	Close              string
	NotQueued          string
	StateReason        string
	Version            string
	QueuedAt           string
	ReadyRank          string
	CreatedAt          string
	UpdatedAt          string
	Description        string
	AcceptanceCriteria string
	NoDescription      string
	NoAcceptance       string

	KeyFacts    string
	FactKinds   map[domain.FactKind]string
	Facts       string
	FactsHint   string
	NoFacts     string
	FactData    string
	Events      string
	NoEvents    string
	EventTypes  map[domain.EventType]string
	EventsLimit string
	EventTime   string
	EventType   string
	EventActor  string
	EventVer    string
	EventDetail string

	NewTaskHeading     string
	FieldTitle         string
	CreateTask         string
	Cancel             string
	EditTask           string
	SaveChanges        string
	SetState           string
	TargetState        string
	Reason             string
	RecordState        string
	StateHint          string
	RecordFact         string
	FactKind           string
	FactBody           string
	FactDataField      string
	FactCoreFields     string
	FactBaseline       string
	FactFingerprint    string
	FactAcceptedCommit string
	FactVerdict        string
	FactProvenance     string
	FactProvRole       string
	FactProvSession    string
	FactProvHarness    string
	FactProvModel      string
	FactProvMissing    string
	SubmitFact         string

	Notices      map[string]string
	Errors       map[string]string
	ErrorUnknown string
	FieldNames   map[string]string
	FieldPrefix  string
	FieldSuffix  string

	LiveUpdated string
	LiveStale   string
}

// zhCN is the default locale.
var zhCN = UIStrings{
	ProductName:   "Agent Board",
	BoardSubtitle: "AI 研发任务看板",
	NewTask:       "+ 新建任务",
	ProjectLabel:  "项目",
	ActorLabel:    "操作者",

	Columns: map[domain.State]string{
		domain.StateReady:      "待开始",
		domain.StateInProgress: "进行中",
		domain.StateDone:       "已完成",
		domain.StateBlocked:    "已阻塞",
	},
	NoCards:              "暂无任务",
	DoneTotalLabel:       "当前已完成",
	ViewAllCompleted:     "查看全部已完成",
	NoCompleted:          "暂无已完成任务",
	CompletedTitle:       "已完成任务",
	CompletedTotalLabel:  "当前 DONE 总数",
	CompletedAt:          "完成时间",
	CompletedTimeMissing: "时间缺失",
	BackHome:             "返回首页",
	Pagination:           "已完成任务分页",
	FirstPage:            "第一页",
	PreviousPage:         "上一页",
	NextPage:             "下一页",
	LastPage:             "末页",
	PageLabel:            "页码",

	Unqueued:     "未入队任务",
	UnqueuedHint: "未入队的任务没有生命周期状态，不属于任何一列；入队后进入 READY。",
	NoUnqueued:   "没有未入队的任务",
	Queue:        "入队",
	MoveUp:       "上移",
	MoveDown:     "下移",

	Close:              "关闭",
	NotQueued:          "未入队",
	StateReason:        "状态原因",
	Version:            "版本",
	QueuedAt:           "入队时间",
	ReadyRank:          "READY 排序值",
	CreatedAt:          "创建时间",
	UpdatedAt:          "更新时间",
	Description:        "任务说明",
	AcceptanceCriteria: "验收标准",
	NoDescription:      "未填写任务说明",
	NoAcceptance:       "未填写验收标准",

	KeyFacts: "最新执行 / 交付 / 验证 / 决策 / 交接",
	FactKinds: map[domain.FactKind]string{
		domain.FactExecution:    "执行",
		domain.FactDelivery:     "交付",
		domain.FactVerification: "验证",
		domain.FactDecision:     "决策",
		domain.FactHandoff:      "交接",
		domain.FactNote:         "备注",
	},
	Facts:     "事实记录",
	FactsHint: "事实只追加记录，不会改变任务状态。",
	NoFacts:   "暂无事实记录",
	FactData:  "结构化数据",
	Events:    "事件历史",
	NoEvents:  "暂无事件记录",
	EventTypes: map[domain.EventType]string{
		domain.EventTaskCreated:    "创建任务",
		domain.EventTaskUpdated:    "编辑任务",
		domain.EventTaskQueued:     "入队",
		domain.EventTaskStateSet:   "记录状态",
		domain.EventReadyReordered: "调整 READY 排序",
		domain.EventFactRecorded:   "记录事实",
	},
	EventsLimit: "仅显示最早的 1000 条事件，完整历史请用 CLI：aboard events --task",
	EventTime:   "时间",
	EventType:   "事件",
	EventActor:  "操作者",
	EventVer:    "版本",
	EventDetail: "内容",

	NewTaskHeading:     "新建任务",
	FieldTitle:         "标题",
	CreateTask:         "创建任务",
	Cancel:             "取消",
	EditTask:           "编辑任务",
	SaveChanges:        "保存修改",
	SetState:           "修改状态",
	TargetState:        "目标状态",
	Reason:             "原因（可选，例如阻塞原因）",
	RecordState:        "记录状态",
	StateHint:          "看板如实记录所选状态，不做流转校验；何时修改由工作流 Skill 决定。",
	RecordFact:         "记录事实",
	FactKind:           "类型",
	FactBody:           "内容",
	FactDataField:      "结构化数据（可选，JSON 对象）",
	FactCoreFields:     "交付 / 验证核心字段",
	FactBaseline:       "基线",
	FactFingerprint:    "指纹",
	FactAcceptedCommit: "已接受提交",
	FactVerdict:        "验证结论",
	FactProvenance:     "来源信息",
	FactProvRole:       "角色",
	FactProvSession:    "会话",
	FactProvHarness:    "Harness",
	FactProvModel:      "模型",
	FactProvMissing:    "未记录来源信息",
	SubmitFact:         "记录",

	Notices: map[string]string{
		"created":   "任务已创建。新任务尚未入队。",
		"updated":   "任务已更新。",
		"queued":    "任务已入队，进入 READY。",
		"state":     "任务状态已记录。",
		"reordered": "READY 排序已更新。",
		"fact":      "事实已记录。",
	},
	Errors: map[string]string{
		domain.CodeVersionConflict:     "任务已被他人修改，本次操作未生效。已显示最新数据，请确认后重试。",
		domain.CodeReadyOrderConflict:  "READY 队列已变化，本次排序未生效。已显示最新顺序，请重试。",
		domain.CodeInvalidArgument:     "提交的内容未通过校验，操作未生效。",
		domain.CodeTaskNotFound:        "任务不存在。",
		domain.CodeTaskNotQueued:       "任务尚未入队，没有生命周期状态；请先入队。",
		domain.CodeAlreadyQueued:       "任务已经入队。",
		domain.CodeIdempotencyConflict: "重复提交的内容与先前请求不一致，操作未生效。请刷新后重试。",
		domain.CodeStorageBusy:         "存储繁忙，操作未生效，请稍后重试。",
	},
	ErrorUnknown: "操作未能完成。",
	FieldNames: map[string]string{
		"title":            "标题",
		"body":             "内容",
		"data":             "结构化数据",
		"kind":             "类型",
		"state":            "状态",
		"expected_version": "版本",
		"tasks":            "READY 排序",
		"actor":            "操作者",
		"arguments":        "表单字段",
	},
	FieldPrefix: "（字段：",
	FieldSuffix: "）",

	LiveUpdated: "看板已在别处更新；当前正在编辑，提交或刷新后显示最新数据。",
	LiveStale:   "暂时无法连接看板服务，显示的可能不是最新数据。",
}
