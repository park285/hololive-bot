package youtubejs

import (
	"errors"
	"fmt"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const (
	StateUnconfigured = "UNCONFIGURED"
	StateReady        = "READY"
	StateDraining     = "DRAINING"
	StateStopped      = "STOPPED"
	StateFaulted      = "FAULTED"
)

type BootstrapLimits struct {
	RequestBodyBytes  int64 `json:"request_body_bytes"`
	ResponseBodyBytes int64 `json:"response_body_bytes"`
	MaxInflight       int   `json:"max_inflight"`
}

type BootstrapRequest struct {
	ProtocolVersion int16           `json:"protocol_version"`
	Limits          BootstrapLimits `json:"limits"`
}

type BootstrapResponse struct {
	ProtocolVersion   int16  `json:"protocol_version"`
	State             string `json:"state"`
	RequestBodyBytes  int64  `json:"request_body_bytes"`
	ResponseBodyBytes int64  `json:"response_body_bytes"`
	MaxInflight       int    `json:"max_inflight"`
}

type HealthResponse struct {
	ProtocolVersion int16        `json:"protocol_version"`
	State           string       `json:"state"`
	Inflight        int          `json:"inflight"`
	MaxInflight     int          `json:"max_inflight"`
	Proof           *ProofStatus `json:"proof,omitempty"`
}

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
	ScheduleLookups *int       `json:"schedule_lookups,omitempty"`
}

type Pagination struct {
	PageCount         int               `json:"page_count"`
	Exhausted         bool              `json:"exhausted"`
	Continuity        string            `json:"continuity"`
	TerminationReason TerminationReason `json:"termination_reason"`
}

func (p Pagination) Validate() error {
	if p.PageCount < 1 || p.PageCount > 100 {
		return errors.New("validate pagination: page_count must be between 1 and 100")
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

type CommunityRequest struct {
	ProtocolVersion         int16  `json:"protocol_version"`
	ChannelID               string `json:"channel_id"`
	MaxResults              int    `json:"max_results"`
	MaxPages                int    `json:"max_pages"`
	MaxSuccessResponseBytes int    `json:"max_success_response_bytes"`
}

type CommunityThumbnail struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type CommunityPost struct {
	PostID         string               `json:"postId"`
	UpstreamPostID string               `json:"upstreamPostId,omitempty"`
	AuthorID       string               `json:"authorId"`
	AuthorName     string               `json:"authorName"`
	AuthorPhoto    []CommunityThumbnail `json:"authorPhoto"`
	ContentText    string               `json:"contentText"`
	PublishedText  string               `json:"publishedText"`
	PublishedAt    *time.Time           `json:"publishedAt,omitempty"`
	LikeCount      int64                `json:"likeCount"`
	CommentCount   int64                `json:"commentCount"`
	Images         []CommunityThumbnail `json:"images,omitempty"`
	VideoID        string               `json:"videoId,omitempty"`
}

type CommunityResult struct {
	ProtocolMeta
	Pagination

	Posts      []*CommunityPost `json:"posts"`
	MissingTab bool             `json:"missing_tab,omitzero"`
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
	VideoID    string `json:"video_id"`
	ChannelID  string `json:"channel_id"`
	Title      string `json:"title"`
	IsUpcoming bool   `json:"is_upcoming,omitzero"`
}

type ContentResult struct {
	ProtocolMeta
	Pagination

	Items      []ContentItem `json:"items"`
	MissingTab bool          `json:"missing_tab,omitzero"`
}

func (r *ContentResult) protocolMetadata() ProtocolMeta { return r.ProtocolMeta }
func (r *ContentResult) validateSuccess() error         { return r.Validate() }

const ChannelLiveMaxScheduleLookups = 32

type ChannelRequest struct {
	ProtocolVersion         int16      `json:"protocol_version"`
	ChannelID               string     `json:"channel_id"`
	Kind                    string     `json:"kind"`
	MaxScheduleLookups      int        `json:"max_schedule_lookups,omitzero"`
	MaxSuccessResponseBytes int        `json:"max_success_response_bytes"`
	ScheduledFor            *time.Time `json:"scheduled_for,omitzero"`
}

type LiveSessionItem struct {
	VideoID            string     `json:"video_id"`
	ChannelID          string     `json:"channel_id"`
	Status             string     `json:"status"`
	Title              string     `json:"title,omitempty"`
	ThumbnailURL       string     `json:"thumbnail_url,omitempty"`
	ScheduledAt        *time.Time `json:"scheduled_at,omitempty"`
	ScheduleObservedAt *time.Time `json:"schedule_observed_at,omitzero"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
}

type ChannelProfileItem struct {
	Handle      *string `json:"handle,omitzero"`
	Description *string `json:"description,omitzero"`
	Country     *string `json:"country,omitzero"`
	JoinedDate  *string `json:"joined_date,omitzero"`
}

type ChannelPhotoVariant struct {
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type UnavailableLiveSession struct {
	VideoID   string `json:"video_id"`
	ChannelID string `json:"channel_id"`
	Reason    string `json:"reason"`
}

type ChannelResult struct {
	ProtocolMeta
	Pagination

	LiveSessions            []LiveSessionItem             `json:"live_sessions"`
	UnavailableLiveSessions []UnavailableLiveSession      `json:"unavailable_live_sessions,omitempty"`
	Profile                 ChannelProfileItem            `json:"profile"`
	Photo                   []ChannelPhotoVariant         `json:"photo"`
	MissingTab              bool                          `json:"missing_tab,omitzero"`
	ScheduleLookups         *int                          `json:"schedule_lookups,omitempty"`
	LiveQuery               *contract.LiveSnapshotQueryV1 `json:"live_query,omitempty"`
}

func (r *ChannelResult) protocolMetadata() ProtocolMeta { return r.ProtocolMeta }

func (r *ChannelResult) validateSuccess() error {
	if err := r.Validate(); err != nil {
		return err
	}

	if (r.LiveQuery != nil) != (r.ScheduleLookups != nil) {
		return errors.New("youtube.js helper schedule lookups must accompany the live query")
	}

	if r.ScheduleLookups != nil && (*r.ScheduleLookups < 0 || *r.ScheduleLookups > ChannelLiveMaxScheduleLookups) {
		return errors.New("youtube.js helper schedule lookups are outside the protocol limit")
	}

	return nil
}

const MaxLiveCheckResponseBytes = 4 << 10

type ChannelLiveCheckRequest struct {
	ProtocolVersion         int16  `json:"protocol_version"`
	ChannelID               string `json:"channel_id"`
	MaxSuccessResponseBytes int    `json:"max_success_response_bytes"`
}

type ChannelLiveCheckResult struct {
	ProtocolMeta

	ChannelID                string                           `json:"channel_id"`
	Outcome                  contract.ChannelLiveCheckOutcome `json:"outcome"`
	SelectedVideoID          string                           `json:"selected_video_id,omitempty"`
	ChannelIdentityConfirmed bool                             `json:"channel_identity_confirmed"`
	UnknownReason            contract.LiveCheckUnknownReason  `json:"unknown_reason,omitempty"`
}

func (r *ChannelLiveCheckResult) protocolMetadata() ProtocolMeta { return r.ProtocolMeta }

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

type VideoLiveCheckRequest struct {
	ProtocolVersion         int16  `json:"protocol_version"`
	VideoID                 string `json:"video_id"`
	MaxSuccessResponseBytes int    `json:"max_success_response_bytes"`
}

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
	ScheduledAt             *time.Time                       `json:"scheduled_at,omitempty"`
	WaitingStateConfirmed   *bool                            `json:"waiting_state_confirmed,omitempty"`
	EndedAt                 *time.Time                       `json:"ended_at,omitempty"`
	PublishedAt             *time.Time                       `json:"published_at,omitempty"`
	Availability            contract.VideoAvailability       `json:"availability"`
	Method                  contract.VideoAvailabilityMethod `json:"method"`
	UnknownReason           contract.LiveCheckUnknownReason  `json:"unknown_reason,omitempty"`
}

func (r *VideoLiveCheckResult) protocolMetadata() ProtocolMeta { return r.ProtocolMeta }

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
	return reason != contract.LiveCheckReasonAvailabilityUnclassified && liveCheckReason(reason)
}

func videoLiveCheckReason(reason contract.LiveCheckUnknownReason) bool {
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
