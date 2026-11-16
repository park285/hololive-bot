package scheduleposter

import (
	"context"
	"errors"
	"time"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
)

type changeStore interface {
	PublishedRows(ctx context.Context, channelID string, day time.Time) (posterstore.PublishedSchedule, bool, error)
	PendingPosts(ctx context.Context, schedule posterstore.PublishedSchedule, limit int) ([]posterstore.Post, error)
	RecordDecision(ctx context.Context, postID int64, decision posterstore.PostDecision) (bool, error)
	RecordMatch(ctx context.Context, schedule posterstore.PublishedSchedule, post posterstore.Post, result posterstore.MatchResult) (bool, error)
}

type postLedger interface {
	InsertPosts(ctx context.Context, posterID int64, posts []posterstore.Post) (int64, error)
	PurgePosts(ctx context.Context, before time.Time, limit int) (int64, error)
}

type postClassifier interface {
	classifyPost(ctx context.Context, body string) (postAnswer, usage, error)
}

type postMatcher interface {
	matchPost(ctx context.Context, prompt string) ([]matchItem, usage, error)
}

type postAnswer struct {
	ScheduleInfo float64
	Kind         string
}

// matchItem은 대응 모델이 돌려준 항목이다. 글 본문에서 나온 신뢰할 수 없는 값이라 judgeItems가 검증한 뒤에만 쓴다.
type matchItem struct {
	Row      *int                  `json:"row"`
	Date     string                `json:"date"`
	Start    *string               `json:"start"`
	TZ       *string               `json:"tz"`
	Status   posterstore.RowStatus `json:"status"`
	Text     string                `json:"text"`
	Evidence string                `json:"evidence"`
}

// errChangeServiceFailed는 어느 글도 처리할 수 없는 실패다(키·권한·할당량·요청 꼴·연결 실패). 기록하지 않고 그 tick의 남은 대상을 모두 멈춘다.
var errChangeServiceFailed = errors.New("schedule poster change detection service failed")

// errChangeOutcomeUnknown은 글 탓인지 알 수 없는 실패다. 기록하지 않고 그 멤버를 멈추며, 종단 기준이 지난 글은 reasonUnavailable로 닫는다.
var errChangeOutcomeUnknown = errors.New("schedule poster change outcome is unknown")

// errMatchBlocked는 Vertex가 대응 입력을 차단한 것이다. 다시 보내도 같으므로 재시도하지 않는다.
var errMatchBlocked = errors.New("schedule poster match input blocked")

// errBadAnswer는 모델 응답을 해석할 수 없거나 스키마를 어긴 것이다. 대응은 재시도 한 묶음의 모든 시도가 이 실패일 때만 reasonBadAnswer로 닫는다.
var errBadAnswer = errors.New("schedule poster model answer is malformed")

// postFaultError는 글 때문에 처리할 수 없다는 뜻이다. 다시 보내도 같으므로 재시도하지 않고 reason으로 바로 닫는다.
type postFaultError struct {
	reason string
	err    error
}

func (f *postFaultError) Error() string {
	if f.err == nil {
		return "schedule poster post failed: " + f.reason
	}

	return "schedule poster post failed: " + f.reason + ": " + f.err.Error()
}

func (f *postFaultError) Unwrap() error {
	return f.err
}

const (
	reasonUnavailable    = "unavailable"
	reasonBlocked        = "blocked"
	reasonBadAnswer      = "bad_answer"
	reasonInvalidChanges = "invalid_changes"
)

const (
	rejectFormat        = "format"
	rejectForeignZone   = "foreign_zone"
	rejectEvidence      = "evidence"
	rejectTextMarker    = "text_marker"
	rejectNoSuchRow     = "no_such_row"
	rejectDateRange     = "date_range"
	rejectPastTime      = "past_time"
	rejectSlotLimit     = "slot_limit"
	rejectDuplicateRow  = "duplicate_row"
	rejectTimeCollision = "time_collision"
	rejectNoContent     = "no_content"
	rejectRowDate       = "row_date"
)
