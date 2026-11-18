package dispatchops

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

const (
	PageSize              = 50
	FailureBreakdownLimit = 10
	MaxReplaySize         = 100
)

var (
	ErrInvalidInput = errors.New("invalid dispatch operation input")
	ErrNotFound     = errors.New("dispatch delivery not found")
	ErrConflict     = errors.New("dispatch delivery changed or cannot be replayed")
	ErrUnavailable  = errors.New("dispatch operations unavailable")
)

var statuses = [...]string{
	contractsalarm.DispatchStatusPending,
	contractsalarm.DispatchStatusRetry,
	contractsalarm.DispatchStatusLeased,
	contractsalarm.DispatchStatusSending,
	contractsalarm.DispatchStatusSent,
	contractsalarm.DispatchStatusDLQ,
	contractsalarm.DispatchStatusQuarantined,
	contractsalarm.DispatchStatusCancelled,
}

var errorCodeFilter = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type Delivery struct {
	ID            string     `json:"id"`
	EventID       string     `json:"eventId"`
	RoomID        string     `json:"roomId"`
	SendUnitID    string     `json:"sendUnitId"`
	AlarmType     string     `json:"alarmType"`
	ChannelID     string     `json:"channelId"`
	StreamID      string     `json:"streamId"`
	Status        string     `json:"status"`
	AttemptCount  int        `json:"attemptCount"`
	ErrorCode     string     `json:"errorCode"`
	NextAttemptAt time.Time  `json:"nextAttemptAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	LockExpiresAt *time.Time `json:"lockExpiresAt"`
	SendingAt     *time.Time `json:"sendingAt"`
	SentAt        *time.Time `json:"sentAt"`
	DLQAt         *time.Time `json:"dlqAt"`
	QuarantinedAt *time.Time `json:"quarantinedAt"`
	CancelledAt   *time.Time `json:"cancelledAt"`
}

type Filter struct{ Status, RoomID, ChannelID, BeforeID, AlarmType, ErrorCode string }

type Page struct {
	Items        []Delivery `json:"items"`
	NextBeforeID string     `json:"nextBeforeId"`
}

type StatusCount struct {
	Status   string    `json:"status"`
	Count    string    `json:"count"`
	OldestAt time.Time `json:"oldestAt"`
}

type Summary struct {
	Counts     []StatusCount `json:"counts"`
	ObservedAt time.Time     `json:"observedAt"`
}

type FailureCount struct {
	Value string `json:"value"`
	Count string `json:"count"`
}

type FailureBreakdown struct {
	Total      string         `json:"total"`
	ErrorCodes []FailureCount `json:"errorCodes"`
	AlarmTypes []FailureCount `json:"alarmTypes"`
	Channels   []FailureCount `json:"channels"`
	Rooms      []FailureCount `json:"rooms"`
	ObservedAt time.Time      `json:"observedAt"`
}

type Revision struct {
	ID        string    `json:"id"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Detail struct {
	Delivery       Delivery   `json:"delivery"`
	Group          []Delivery `json:"group"`
	GroupTruncated bool       `json:"groupTruncated"`
	ReplayTargets  []Revision `json:"replayTargets"`
	ReplayBlocked  string     `json:"replayBlocked"`
}

type RequeueRequest struct {
	OperatorID       string     `json:"operatorId"`
	Reason           string     `json:"reason"`
	DuplicateRiskAck bool       `json:"duplicateRiskAck"`
	Targets          []Revision `json:"targets"`
}

type RequeueResult struct {
	IDs []string `json:"ids"`
}

type Action struct {
	ID               string    `json:"id"`
	DeliveryID       string    `json:"deliveryId"`
	Action           string    `json:"action"`
	OperatorID       string    `json:"operatorId"`
	Reason           string    `json:"reason"`
	FromStatus       string    `json:"fromStatus"`
	ToStatus         string    `json:"toStatus"`
	DuplicateRiskAck bool      `json:"duplicateRiskAck"`
	CreatedAt        time.Time `json:"createdAt"`
}

type ActionPage struct {
	Items        []Action `json:"items"`
	NextBeforeID string   `json:"nextBeforeId"`
}

func ParseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0, fmt.Errorf("delivery id: %w", ErrInvalidInput)
	}

	return id, nil
}

func (f Filter) Validate() error {
	if f.Status != "" && !knownStatus(f.Status) {
		return fmt.Errorf("status: %w", ErrInvalidInput)
	}

	if !validText(f.RoomID, 100) || !validText(f.ChannelID, 64) || !validText(f.AlarmType, 64) {
		return fmt.Errorf("filter: %w", ErrInvalidInput)
	}

	if f.ErrorCode != "" && !errorCodeFilter.MatchString(f.ErrorCode) {
		return fmt.Errorf("error code: %w", ErrInvalidInput)
	}

	if f.BeforeID != "" {
		if _, err := ParseID(f.BeforeID); err != nil {
			return err
		}
	}

	return nil
}

func knownStatus(status string) bool {
	for _, allowed := range statuses {
		if status == allowed {
			return true
		}
	}

	return false
}

func validText(value string, maxRunes int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxRunes &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

func (r RequeueRequest) Validate(id string) error {
	if _, err := ParseID(id); err != nil {
		return err
	}

	if err := r.validateAudit(); err != nil {
		return err
	}

	return validateTargets(r.Targets, id)
}

func (r RequeueRequest) validateAudit() error {
	if !r.DuplicateRiskAck || r.OperatorID == "" || r.Reason == "" || !validText(r.OperatorID, 128) || !validText(r.Reason, 1024) {
		return fmt.Errorf("operator, reason or acknowledgement: %w", ErrInvalidInput)
	}

	return nil
}

func validateTargets(targets []Revision, addressedID string) error {
	if len(targets) == 0 || len(targets) > MaxReplaySize {
		return fmt.Errorf("target count: %w", ErrInvalidInput)
	}

	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if _, err := ParseID(target.ID); err != nil {
			return err
		}

		if target.UpdatedAt.IsZero() || target.UpdatedAt.Year() < 1 || target.UpdatedAt.Year() > 9999 {
			return fmt.Errorf("revision: %w", ErrInvalidInput)
		}

		if _, duplicate := seen[target.ID]; duplicate {
			return fmt.Errorf("duplicate target: %w", ErrInvalidInput)
		}

		seen[target.ID] = struct{}{}
	}

	if _, found := seen[addressedID]; !found {
		return fmt.Errorf("addressed delivery missing: %w", ErrInvalidInput)
	}

	return nil
}

func replayBlock(group []Delivery) string {
	if len(group) == 0 {
		return "not_found"
	}

	if len(group) > MaxReplaySize {
		return "group_too_large"
	}

	first := &group[0]
	for index := range group {
		item := &group[index]

		if (item.Status != contractsalarm.DispatchStatusDLQ && item.Status != contractsalarm.DispatchStatusQuarantined) ||
			item.SentAt != nil || item.CancelledAt != nil {
			return "group_not_terminal_failure"
		}

		if item.RoomID != first.RoomID || item.SendUnitID != first.SendUnitID {
			return "group_identity_mismatch"
		}
	}

	return ""
}

func validateReplay(group []Delivery, request RequeueRequest) error {
	if replayBlock(group) != "" {
		return ErrConflict
	}

	return validateRevisions(group, request.Targets)
}

func validateRevisions(group []Delivery, targets []Revision) error {
	if len(group) != len(targets) {
		return ErrConflict
	}

	revisions := make(map[string]time.Time, len(targets))
	for _, target := range targets {
		revisions[target.ID] = target.UpdatedAt
	}

	for index := range group {
		item := &group[index]

		if expected, ok := revisions[item.ID]; !ok || !item.UpdatedAt.Equal(expected) {
			return ErrConflict
		}
	}

	return nil
}
