package youtubejs

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

const (
	StateUnconfigured = "UNCONFIGURED"
	StateReady        = "READY"
	StateDraining     = "DRAINING"
	StateStopped      = "STOPPED"
	StateFaulted      = "FAULTED"
)

type BootstrapProxy struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url,omitempty"`
}

type BootstrapLimits struct {
	RequestBodyBytes  int64 `json:"request_body_bytes"`
	ResponseBodyBytes int64 `json:"response_body_bytes"`
	MaxInflight       int   `json:"max_inflight"`
}

type BootstrapRequest struct {
	ProtocolVersion int16           `json:"protocol_version"`
	Proxy           BootstrapProxy  `json:"proxy"`
	Limits          BootstrapLimits `json:"limits"`
}

type BootstrapResponse struct {
	ProtocolVersion   int16  `json:"protocol_version"`
	State             string `json:"state"`
	ProxyEnabled      bool   `json:"proxy_enabled"`
	RequestBodyBytes  int64  `json:"request_body_bytes"`
	ResponseBodyBytes int64  `json:"response_body_bytes"`
	MaxInflight       int    `json:"max_inflight"`
}

type HealthResponse struct {
	ProtocolVersion int16        `json:"protocol_version"`
	State           string       `json:"state"`
	Inflight        int          `json:"inflight"`
	MaxInflight     int          `json:"max_inflight"`
	ProxyEnabled    bool         `json:"proxy_enabled"`
	Proof           *ProofStatus `json:"proof,omitempty"`
}

// ProofStatus는 발급 상태 진단이며 방송 상태나 helper readiness의 증거가 아닙니다.
type ProofStatus struct {
	State              string `json:"state"`
	Generation         string `json:"generation,omitempty"`
	ExpiresAt          string `json:"expires_at,omitempty"`
	NextAttemptAt      string `json:"next_attempt_at,omitempty"`
	LastError          string `json:"last_error,omitempty"`
	CleanupError       string `json:"cleanup_error,omitempty"`
	BootstrapAttempts  uint64 `json:"bootstrap_attempts"`
	BootstrapSuccesses uint64 `json:"bootstrap_successes"`
	UpstreamRequests   uint64 `json:"upstream_requests"`
	MintedTotal        uint64 `json:"minted_total"`
	AttachedTotal      uint64 `json:"attached_total"`
}

type ProtocolMeta struct {
	ProtocolVersion int16 `json:"protocol_version"`
}

type (
	RPCErrorCode      string
	RPCFailureClass   string
	RPCRetryKind      string
	TerminationReason string
)

const (
	TerminationExhausted               TerminationReason = "exhausted"
	TerminationMaxPages                TerminationReason = "max_pages"
	TerminationMaxResults              TerminationReason = "max_results"
	TerminationMaxSuccessResponseBytes TerminationReason = "max_success_response_bytes"
	TerminationCursorLoop              TerminationReason = "cursor_loop"
	TerminationContinuationTransient   TerminationReason = "continuation_transient"
)

type RPCRetryHint struct {
	Kind    RPCRetryKind `json:"kind"`
	AfterMS int64        `json:"after_ms"`
	At      string       `json:"at,omitempty"`
}

type RPCFailure struct {
	Code    RPCErrorCode    `json:"code"`
	Class   RPCFailureClass `json:"class"`
	Retry   RPCRetryHint    `json:"retry"`
	Message string          `json:"message"`
}

type RPCErrorBody struct {
	ProtocolVersion int16      `json:"protocol_version"`
	Error           RPCFailure `json:"error"`
}

type Pagination struct {
	PageCount         int               `json:"page_count"`
	CursorStart       string            `json:"cursor_start,omitempty"`
	CursorEnd         string            `json:"cursor_end,omitempty"`
	Exhausted         bool              `json:"exhausted"`
	Continuity        string            `json:"continuity"`
	TerminationReason TerminationReason `json:"termination_reason"`
}

func (p Pagination) Validate() error {
	if p.PageCount < 1 || p.PageCount > 100 {
		return errors.New("validate pagination: page_count must be between 1 and 100")
	}

	if err := validateCursor("cursor_start", p.CursorStart); err != nil {
		return err
	}

	if err := validateCursor("cursor_end", p.CursorEnd); err != nil {
		return err
	}

	return validatePaginationTermination(p)
}

var paginationValidators = map[TerminationReason]func(Pagination) error{
	TerminationExhausted:               validateExhaustedPagination,
	TerminationMaxPages:                validatePartialPagination,
	TerminationMaxResults:              validatePartialPagination,
	TerminationMaxSuccessResponseBytes: validatePartialPagination,
	TerminationCursorLoop:              validateInterruptedPagination,
	TerminationContinuationTransient:   validateInterruptedPagination,
}

func validatePaginationTermination(p Pagination) error {
	validate, ok := paginationValidators[p.TerminationReason]
	if !ok {
		return errors.New("validate pagination: termination_reason is invalid")
	}

	return validate(p)
}

func validateExhaustedPagination(p Pagination) error {
	if !p.Exhausted {
		return errors.New("validate pagination: exhausted reason requires exhausted=true")
	}

	if p.Continuity != string(contract.ContinuityContiguous) && p.Continuity != string(contract.ContinuityNotApplicable) {
		return errors.New("validate pagination: exhausted reason has invalid continuity")
	}

	return nil
}

func validatePartialPagination(p Pagination) error {
	if p.Exhausted {
		return errors.New("validate pagination: partial reason requires exhausted=false")
	}

	if p.Continuity != string(contract.ContinuityGapUnresolved) && p.Continuity != string(contract.ContinuityNotApplicable) {
		return errors.New("validate pagination: partial reason has invalid continuity")
	}

	return nil
}

func validateInterruptedPagination(p Pagination) error {
	if p.Exhausted || p.Continuity != string(contract.ContinuityGapUnresolved) {
		return errors.New("validate pagination: interrupted reason requires unresolved continuity")
	}

	return nil
}

func (p Pagination) Quality() (contract.Completeness, contract.Continuity, error) {
	if err := p.Validate(); err != nil {
		return "", "", err
	}

	continuity := contract.Continuity(p.Continuity)
	if p.TerminationReason == TerminationExhausted {
		return contract.CompletenessComplete, continuity, nil
	}

	return contract.CompletenessPartial, continuity, nil
}

func validateCursor(field, cursor string) error {
	if jsonStringBytes(cursor) > 8192 {
		return fmt.Errorf("validate pagination: %s exceeds 8192 bytes", field)
	}

	return nil
}

func jsonStringBytes(value string) int {
	size := 2

	for _, r := range value {
		size += jsonRuneBytes(r)
	}

	return size
}

func jsonRuneBytes(r rune) int {
	switch r {
	case '"', '\\', '\b', '\f', '\n', '\r', '\t':
		return 2
	default:
		if r < 0x20 {
			return 6
		}

		return utf8.RuneLen(r)
	}
}

type CommunityRequest struct {
	ProtocolVersion         int16  `json:"protocol_version"`
	ChannelID               string `json:"channel_id"`
	MaxResults              int    `json:"max_results"`
	MaxPages                int    `json:"max_pages"`
	MaxSuccessResponseBytes int    `json:"max_success_response_bytes"`
}

type CommunityResult struct {
	ProtocolMeta
	Pagination

	Posts      []*parser.CommunityPost `json:"posts"`
	MissingTab bool                    `json:"missing_tab"`
}

func (r *CommunityResult) protocolMetadata() ProtocolMeta { return r.ProtocolMeta }
func (r *CommunityResult) validateSuccess() error         { return r.Validate() }

type ContentRequest struct {
	ProtocolVersion         int16  `json:"protocol_version"`
	ChannelID               string `json:"channel_id"`
	Kind                    string `json:"kind"`
	MaxResults              int    `json:"max_results"`
	MaxPages                int    `json:"max_pages"`
	MaxSuccessResponseBytes int    `json:"max_success_response_bytes"`
}

type ContentItem struct {
	VideoID      string     `json:"video_id"`
	ChannelID    string     `json:"channel_id"`
	Title        string     `json:"title"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	ScheduledFor *time.Time `json:"scheduled_for,omitempty"`
	IsPremiere   *bool      `json:"is_premiere,omitempty"`
}

type ContentResult struct {
	ProtocolMeta
	Pagination

	Items      []ContentItem `json:"items"`
	MissingTab bool          `json:"missing_tab"`
}

func (r *ContentResult) protocolMetadata() ProtocolMeta { return r.ProtocolMeta }
func (r *ContentResult) validateSuccess() error         { return r.Validate() }

// ChannelRequest는 동일 bundle의 helper에 live 또는 metadata 수집만 요청합니다.
type ChannelRequest struct {
	ProtocolVersion         int16  `json:"protocol_version"`
	ChannelID               string `json:"channel_id"`
	Kind                    string `json:"kind"`
	MaxPages                int    `json:"max_pages"`
	MaxSuccessResponseBytes int    `json:"max_success_response_bytes"`
}

type LiveSessionItem struct {
	VideoID      string     `json:"video_id"`
	ChannelID    string     `json:"channel_id"`
	Status       string     `json:"status"`
	Title        string     `json:"title,omitempty"`
	ThumbnailURL string     `json:"thumbnail_url,omitempty"`
	ScheduledAt  *time.Time `json:"scheduled_at,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
}

type ChannelStatsItem struct {
	SubscriberCount *int64 `json:"subscriber_count"`
	ViewCount       *int64 `json:"view_count"`
	VideoCount      *int64 `json:"video_count"`
}

type ChannelProfileItem struct {
	Handle      *string `json:"handle"`
	Description *string `json:"description"`
	Country     *string `json:"country"`
	JoinedDate  *string `json:"joined_date"`
}

type ChannelPhotoVariant struct {
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// UnavailableLiveSession은 시각이 가려진 영상의 관측 실패를 나타내며 canonical 상태를 바꾸지 않습니다.
type UnavailableLiveSession struct {
	VideoID   string `json:"video_id"`
	ChannelID string `json:"channel_id"`
	Reason    string `json:"reason"`
}

// ChannelResult는 수집 범위에 따른 결과와 접근 제한으로 확인하지 못한 영상을 구분합니다.
type ChannelResult struct {
	ProtocolMeta
	Pagination

	LiveSessions            []LiveSessionItem        `json:"live_sessions"`
	UnavailableLiveSessions []UnavailableLiveSession `json:"unavailable_live_sessions,omitempty"`
	Stats                   ChannelStatsItem         `json:"stats"`
	Profile                 ChannelProfileItem       `json:"profile"`
	Photo                   []ChannelPhotoVariant    `json:"photo"`
	MissingTab              bool                     `json:"missing_tab"`
}

func (r *ChannelResult) protocolMetadata() ProtocolMeta { return r.ProtocolMeta }
func (r *ChannelResult) validateSuccess() error         { return r.Validate() }

// MaxLiveCheckResponseBytes는 평탄한 확인 결과 하나의 성공 응답 상한입니다.
// 확인 RPC는 목록 RPC의 응답 예산을 물려받지 않고 이 값 이하로 요청합니다.
const MaxLiveCheckResponseBytes = 4 << 10

// ChannelLiveCheckRequest는 채널 /live 주소 해석 확인 하나를 요청합니다.
type ChannelLiveCheckRequest struct {
	ProtocolVersion         int16  `json:"protocol_version"`
	ChannelID               string `json:"channel_id"`
	MaxSuccessResponseBytes int    `json:"max_success_response_bytes"`
}

// ChannelLiveCheckResult는 pagination 없는 채널 확인 결과입니다. 수집기가 typed coverage를 붙입니다.
type ChannelLiveCheckResult struct {
	ProtocolMeta

	ChannelID                string                           `json:"channel_id"`
	Outcome                  contract.ChannelLiveCheckOutcome `json:"outcome"`
	SelectedVideoID          string                           `json:"selected_video_id,omitempty"`
	ChannelIdentityConfirmed bool                             `json:"channel_identity_confirmed"`
	UnknownReason            contract.LiveCheckUnknownReason  `json:"unknown_reason,omitempty"`
}

func (r *ChannelLiveCheckResult) protocolMetadata() ProtocolMeta { return r.ProtocolMeta }

// validateSuccess는 결과 어휘와 UNKNOWN 사유의 존재 조건만 검사합니다.
// 요청 subject 대조와 세부 판정 모순은 호출자와 공유 envelope 계약이 소유합니다.
func (r *ChannelLiveCheckResult) validateSuccess() error {
	if strings.TrimSpace(r.ChannelID) == "" {
		return errors.New("validate channel live check: channel_id is empty")
	}

	if !r.Outcome.Valid() {
		return fmt.Errorf("validate channel live check: outcome %q is invalid", r.Outcome)
	}

	if (r.Outcome == contract.ChannelLiveCheckUnknown) != (r.UnknownReason != "") {
		return errors.New("validate channel live check: unknown_reason must be present only for UNKNOWN")
	}

	if r.UnknownReason != "" && !channelLiveCheckReason(r.UnknownReason) {
		return fmt.Errorf("validate channel live check: unknown_reason %q is invalid", r.UnknownReason)
	}

	return nil
}

// VideoLiveCheckRequest는 영상 player 확인 하나를 요청합니다.
type VideoLiveCheckRequest struct {
	ProtocolVersion         int16  `json:"protocol_version"`
	VideoID                 string `json:"video_id"`
	MaxSuccessResponseBytes int    `json:"max_success_response_bytes"`
}

// VideoLiveCheckResult는 pagination 없는 영상 확인 결과입니다. 원시 boolean이 없으면 nil로 남습니다.
type VideoLiveCheckResult struct {
	ProtocolMeta

	VideoID                 string                           `json:"video_id"`
	ChannelID               string                           `json:"channel_id,omitempty"`
	IdentityConfirmed       bool                             `json:"identity_confirmed"`
	IsLive                  *bool                            `json:"is_live,omitempty"`
	IsLiveNow               *bool                            `json:"is_live_now,omitempty"`
	IsUpcoming              *bool                            `json:"is_upcoming,omitempty"`
	IsLiveContent           *bool                            `json:"is_live_content,omitempty"`
	IsPrivate               *bool                            `json:"is_private,omitempty"`
	HasLiveBroadcastDetails *bool                            `json:"has_live_broadcast_details,omitempty"`
	StartedAt               *time.Time                       `json:"started_at,omitempty"`
	EndedAt                 *time.Time                       `json:"ended_at,omitempty"`
	Availability            contract.VideoAvailability       `json:"availability"`
	Method                  contract.VideoAvailabilityMethod `json:"method"`
	UnknownReason           contract.LiveCheckUnknownReason  `json:"unknown_reason,omitempty"`
}

func (r *VideoLiveCheckResult) protocolMetadata() ProtocolMeta { return r.ProtocolMeta }

// validateSuccess는 가용성·판정 방법·UNKNOWN 사유의 어휘와 대응만 검사합니다.
// 판정 방법은 availability가 UNKNOWN일 때만 unknown이며 UNKNOWN 사유도 그때만 존재합니다.
func (r *VideoLiveCheckResult) validateSuccess() error {
	if strings.TrimSpace(r.VideoID) == "" {
		return errors.New("validate video live check: video_id is empty")
	}

	method, ok := videoAvailabilityMethods[r.Availability]
	if !ok {
		return fmt.Errorf("validate video live check: availability %q is invalid", r.Availability)
	}

	if r.Method != method {
		return fmt.Errorf("validate video live check: availability %s requires method %q", r.Availability, method)
	}

	if (r.Availability == contract.VideoAvailabilityUnknown) != (r.UnknownReason != "") {
		return errors.New("validate video live check: unknown_reason must be present only for UNKNOWN availability")
	}

	if r.UnknownReason != "" && !videoLiveCheckReason(r.UnknownReason) {
		return fmt.Errorf("validate video live check: unknown_reason %q is invalid", r.UnknownReason)
	}

	return nil
}

var videoAvailabilityMethods = map[contract.VideoAvailability]contract.VideoAvailabilityMethod{
	contract.VideoAvailabilityPublic:            contract.VideoAvailabilityMethodPlayerPublic,
	contract.VideoAvailabilityMembersOnly:       contract.VideoAvailabilityMethodPlayerMembersOnly,
	contract.VideoAvailabilityPublicUnavailable: contract.VideoAvailabilityMethodPlayerPrivate,
	contract.VideoAvailabilityUnknown:           contract.VideoAvailabilityMethodUnknown,
}

func channelLiveCheckReason(reason contract.LiveCheckUnknownReason) bool {
	// 가용성만 미상인 사유는 영상 확인 전용이다.
	return reason != contract.LiveCheckReasonAvailabilityUnclassified && liveCheckReason(reason)
}

func videoLiveCheckReason(reason contract.LiveCheckUnknownReason) bool {
	// not_waiting_state는 채널 /live 예정 연결 판정 전용이다.
	return reason != contract.LiveCheckReasonNotWaitingState && liveCheckReason(reason)
}

func liveCheckReason(reason contract.LiveCheckUnknownReason) bool {
	switch reason {
	case contract.LiveCheckReasonIdentityMissing, contract.LiveCheckReasonIdentityMismatch,
		contract.LiveCheckReasonContradictoryFields, contract.LiveCheckReasonStructureUnrecognized,
		contract.LiveCheckReasonNotWaitingState, contract.LiveCheckReasonLoginRequiredUnclassified,
		contract.LiveCheckReasonErrorUnclassified, contract.LiveCheckReasonRequestFailed,
		contract.LiveCheckReasonAvailabilityUnclassified:
		return true
	default:
		return false
	}
}
