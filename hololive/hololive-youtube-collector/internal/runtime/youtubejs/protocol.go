package youtubejs

import (
	"errors"
	"fmt"
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
	ProtocolVersion int16  `json:"protocol_version"`
	State           string `json:"state"`
	Inflight        int    `json:"inflight"`
	MaxInflight     int    `json:"max_inflight"`
	ProxyEnabled    bool   `json:"proxy_enabled"`
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

	//nolint:wrapcheck // validateCursor가 이미 validate pagination 접두사를 붙인 완결된 메시지를 만든다.
	if err := validateCursor("cursor_start", p.CursorStart); err != nil {
		return err
	}

	//nolint:wrapcheck // validateCursor가 이미 validate pagination 접두사를 붙인 완결된 메시지를 만든다.
	if err := validateCursor("cursor_end", p.CursorEnd); err != nil {
		return err
	}

	//nolint:wrapcheck // validatePaginationTermination이 이미 완결된 메시지를 만든다.
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

	//nolint:wrapcheck // 각 검증 함수가 이미 validate pagination 접두사를 붙인 완결된 메시지를 만든다.
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
		//nolint:wrapcheck // Validate가 이미 어떤 필드가 잘못됐는지 담은 완결된 메시지를 만든다.
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
func (r *CommunityResult) pagination() Pagination         { return r.Pagination }

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
func (r *ContentResult) pagination() Pagination         { return r.Pagination }

type ChannelRequest struct {
	ProtocolVersion         int16  `json:"protocol_version"`
	ChannelID               string `json:"channel_id"`
	MaxPages                int    `json:"max_pages"`
	MaxSuccessResponseBytes int    `json:"max_success_response_bytes"`
}

type LiveSessionItem struct {
	VideoID     string     `json:"video_id"`
	ChannelID   string     `json:"channel_id"`
	Status      string     `json:"status"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
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

type ChannelResult struct {
	ProtocolMeta
	Pagination

	LiveSessions []LiveSessionItem     `json:"live_sessions"`
	Stats        ChannelStatsItem      `json:"stats"`
	Profile      ChannelProfileItem    `json:"profile"`
	Photo        []ChannelPhotoVariant `json:"photo"`
	MissingTab   bool                  `json:"missing_tab"`
}

func (r *ChannelResult) protocolMetadata() ProtocolMeta { return r.ProtocolMeta }
func (r *ChannelResult) pagination() Pagination         { return r.Pagination }

type ViewerRequest struct {
	ProtocolVersion         int16  `json:"protocol_version"`
	VideoID                 string `json:"video_id"`
	MaxSuccessResponseBytes int    `json:"max_success_response_bytes"`
}

type ViewerResult struct {
	ProtocolMeta
	Pagination

	VideoID      string `json:"video_id"`
	ViewerCount  *int64 `json:"viewer_count"`
	Availability string `json:"availability"`
}

func (r *ViewerResult) protocolMetadata() ProtocolMeta { return r.ProtocolMeta }
func (r *ViewerResult) pagination() Pagination         { return r.Pagination }
