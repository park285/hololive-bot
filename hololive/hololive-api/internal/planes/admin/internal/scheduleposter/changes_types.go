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
	RecordDecision(ctx context.Context, channelID string, postID int64, decision posterstore.PostDecision) (bool, error)
	RecordMatch(ctx context.Context, schedule posterstore.PublishedSchedule, post posterstore.Post, result posterstore.MatchResult) (bool, error)
}

type postLedger interface {
	InsertPosts(ctx context.Context, posterID int64, posts []posterstore.Post) (int64, error)
	PurgePosts(ctx context.Context, before time.Time, limit int) (int64, error)
	OfficialStreams(ctx context.Context, videoIDs []string) (map[string]posterstore.OfficialStream, error)
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

type matchItem struct {
	Row      *int                  `json:"row"`
	Date     string                `json:"date"`
	Start    *string               `json:"start"`
	TZ       *string               `json:"tz"`
	Status   posterstore.RowStatus `json:"status"`
	Text     string                `json:"text"`
	Evidence string                `json:"evidence"`
}

var errChangeServiceFailed = errors.New("schedule poster change detection service failed")

var errChangeOutcomeUnknown = errors.New("schedule poster change outcome is unknown")

var errMatchBlocked = errors.New("schedule poster match input blocked")

var errBadAnswer = errors.New("schedule poster model answer is malformed")

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
