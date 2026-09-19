// Package dispatchops는 관리자용 발송 원장 조회와 감사 가능한 재처리를 제공합니다.
package dispatchops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// PageSize는 조회 한 페이지의 최대 행 수입니다.
	PageSize = 50
	// MaxReplaySize는 재처리할 수 있는 외부 발송 묶음의 최대 행 수입니다.
	MaxReplaySize = 100
)

var (
	// ErrInvalidInput은 상태 변경 전에 거부한 잘못된 관리자 입력입니다.
	ErrInvalidInput = errors.New("invalid dispatch operation input")
	// ErrNotFound는 해당 발송 기록이 없음을 나타냅니다.
	ErrNotFound = errors.New("dispatch delivery not found")
	// ErrConflict는 상태나 묶음 구성이 달라져 재처리하지 않았음을 나타냅니다.
	ErrConflict = errors.New("dispatch delivery changed or cannot be replayed")
	// ErrUnavailable은 PostgreSQL 의존성이 연결되지 않았음을 나타냅니다.
	ErrUnavailable = errors.New("dispatch operations unavailable")
)

var statuses = [...]string{"shadowed", "pending", "retry", "leased", "sending", "sent", "dlq", "quarantined", "cancelled"}

// Delivery는 본문과 전송용 내부 식별자를 제외한 발송 상태입니다. bigint는 문자열로 유지합니다.
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

// Filter는 허용된 상태와 정확한 채팅방·채널 식별자 및 ID 커서로 조회 범위를 제한합니다.
// 비어 있는 Status는 DLQ와 격리 상태를 함께 조회합니다.
type Filter struct{ Status, RoomID, ChannelID, BeforeID string }

// Page는 ID 내림차순 조회 결과입니다. nextBeforeId가 비어 있으면 마지막 페이지입니다.
type Page struct {
	Items        []Delivery `json:"items"`
	NextBeforeID string     `json:"nextBeforeId"`
}

// StatusCount는 보존 중인 원장 전체의 상태별 건수와 가장 오래된 생성 시각입니다.
type StatusCount struct {
	Status   string    `json:"status"`
	Count    string    `json:"count"`
	OldestAt time.Time `json:"oldestAt"`
}

// Summary는 보존 중인 전체 원장 집계입니다. 제한 시간을 넘기면 부분 집계 없이 실패합니다.
type Summary struct {
	Counts     []StatusCount `json:"counts"`
	ObservedAt time.Time     `json:"observedAt"`
}

// Revision은 확인한 행의 낙관적 잠금 토큰입니다. updatedAt의 소수초를 보존해야 합니다.
type Revision struct {
	ID        string    `json:"id"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Detail은 현재 발송 항목과 외부 발송 묶음 전체의 재처리 검토 대상입니다.
// replayBlocked가 비어 있을 때만 replayTargets를 그대로 사용해 재처리할 수 있습니다.
type Detail struct {
	Delivery       Delivery   `json:"delivery"`
	Group          []Delivery `json:"group"`
	GroupTruncated bool       `json:"groupTruncated"`
	ReplayTargets  []Revision `json:"replayTargets"`
	ReplayBlocked  string     `json:"replayBlocked"`
}

// RequeueRequest는 묶음 전체에 대한 명시적 재처리 요청입니다.
// OperatorID는 API key 보유자가 전달하는 감사 식별자입니다. gateway에서는 반드시
// 인증된 사용자에 결합해야 하며 브라우저 입력을 그대로 전달해서는 안 됩니다.
type RequeueRequest struct {
	OperatorID       string     `json:"operatorId"`
	Reason           string     `json:"reason"`
	DuplicateRiskAck bool       `json:"duplicateRiskAck"`
	Targets          []Revision `json:"targets"`
}

// RequeueResult는 retry로 변경하고 감사 기록을 남긴 ID입니다. 실제 발송 완료를 뜻하지 않습니다.
type RequeueResult struct {
	IDs []string `json:"ids"`
}

// Action은 발송 원장에 기록된 운영자 행위입니다.
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

// ActionPage는 해당 발송 항목의 감사 이력을 ID 내림차순으로 반환합니다.
type ActionPage struct {
	Items        []Action `json:"items"`
	NextBeforeID string   `json:"nextBeforeId"`
}

// ParseID는 부호·공백·선행 0을 허용하지 않는 양의 PostgreSQL bigint를 검사합니다.
func ParseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0, fmt.Errorf("delivery id: %w", ErrInvalidInput)
	}
	return id, nil
}

// Validate는 허용 목록 밖의 상태와 제어 문자가 섞인 필터를 거부합니다.
func (f Filter) Validate() error {
	if f.Status != "" && !knownStatus(f.Status) {
		return fmt.Errorf("status: %w", ErrInvalidInput)
	}
	if !validText(f.RoomID, 100) || !validText(f.ChannelID, 64) {
		return fmt.Errorf("filter: %w", ErrInvalidInput)
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

func validText(value string, max int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= max &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

// Validate는 필수 확인, 감사 정보, 대상 수, 중복 ID와 리비전을 변경 전에 검사합니다.
func (r RequeueRequest) Validate(id string) error {
	if _, err := ParseID(id); err != nil {
		return err
	}
	if !r.DuplicateRiskAck || r.OperatorID == "" || r.Reason == "" || !validText(r.OperatorID, 128) || !validText(r.Reason, 1024) {
		return fmt.Errorf("operator, reason or acknowledgement: %w", ErrInvalidInput)
	}
	if len(r.Targets) == 0 || len(r.Targets) > MaxReplaySize {
		return fmt.Errorf("target count: %w", ErrInvalidInput)
	}
	seen := make(map[string]struct{}, len(r.Targets))
	for _, target := range r.Targets {
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
	if _, found := seen[id]; !found {
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
	first := group[0]
	for _, item := range group {
		if (item.Status != "dlq" && item.Status != "quarantined") || item.SentAt != nil || item.CancelledAt != nil {
			return "group_not_terminal_failure"
		}
		// 이전 형식의 단일 발송은 자체 묶음입니다. 서로 다른 발송 식별자를 섞지 않습니다.
		if item.RoomID != first.RoomID || item.SendUnitID != first.SendUnitID || (first.SendUnitID == "" && len(group) != 1) {
			return "group_identity_mismatch"
		}
	}
	return ""
}

func validateReplay(group []Delivery, request RequeueRequest) error {
	if replayBlock(group) != "" || len(group) != len(request.Targets) {
		return ErrConflict
	}
	revisions := make(map[string]time.Time, len(request.Targets))
	for _, target := range request.Targets {
		revisions[target.ID] = target.UpdatedAt
	}
	for _, item := range group {
		if expected, ok := revisions[item.ID]; !ok || !item.UpdatedAt.Equal(expected) {
			return ErrConflict
		}
	}
	return nil
}
