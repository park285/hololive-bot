package sourceobservation

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// LiveCheckContractGeneration은 channel_live_check·video_live_check의 첫 계약 세대다.
// 기존 live_snapshot 세대와 독립적으로 증가한다.
const LiveCheckContractGeneration int64 = 1

type ChannelLiveCheckOutcome string

const (
	ChannelLiveCheckLiveVideo     ChannelLiveCheckOutcome = "LIVE_VIDEO"
	ChannelLiveCheckUpcomingVideo ChannelLiveCheckOutcome = "UPCOMING_VIDEO"
	ChannelLiveCheckChannelPage   ChannelLiveCheckOutcome = "CHANNEL_PAGE"
	ChannelLiveCheckUnknown       ChannelLiveCheckOutcome = "UNKNOWN"
)

type LiveCheckUnknownReason string

const (
	LiveCheckReasonIdentityMissing           LiveCheckUnknownReason = "identity_missing"
	LiveCheckReasonIdentityMismatch          LiveCheckUnknownReason = "identity_mismatch"
	LiveCheckReasonContradictoryFields       LiveCheckUnknownReason = "contradictory_fields"
	LiveCheckReasonStructureUnrecognized     LiveCheckUnknownReason = "structure_unrecognized"
	LiveCheckReasonNotWaitingState           LiveCheckUnknownReason = "not_waiting_state"
	LiveCheckReasonLoginRequiredUnclassified LiveCheckUnknownReason = "login_required_unclassified"
	LiveCheckReasonErrorUnclassified         LiveCheckUnknownReason = "error_unclassified"
	LiveCheckReasonRequestFailed             LiveCheckUnknownReason = "request_failed"
	LiveCheckReasonAvailabilityUnclassified  LiveCheckUnknownReason = "availability_unclassified"
)

type VideoAvailability string

const (
	VideoAvailabilityPublic            VideoAvailability = "PUBLIC"
	VideoAvailabilityMembersOnly       VideoAvailability = "MEMBERS_ONLY"
	VideoAvailabilityPublicUnavailable VideoAvailability = "PUBLIC_UNAVAILABLE"
	VideoAvailabilityUnknown           VideoAvailability = "UNKNOWN"
)

type VideoAvailabilityMethod string

const (
	VideoAvailabilityMethodPlayerPublic      VideoAvailabilityMethod = "player_public"
	VideoAvailabilityMethodPlayerMembersOnly VideoAvailabilityMethod = "player_members_only"
	VideoAvailabilityMethodPlayerPrivate     VideoAvailabilityMethod = "player_private"
	VideoAvailabilityMethodUnknown           VideoAvailabilityMethod = "unknown"
)

type ChannelLiveCheckCoverageV1 struct {
	ChannelID string `json:"channel_id"`
}

type VideoLiveCheckCoverageV1 struct {
	VideoID string `json:"video_id"`
}

// ChannelLiveCheckV1은 채널 /live 주소 해석 결과다. LiveQuery 채널 판정 전용이며
// live reducer·pending·absence slot 입력이 아니다.
type ChannelLiveCheckV1 struct {
	ChannelID                string                     `json:"channel_id"`
	Outcome                  ChannelLiveCheckOutcome    `json:"outcome"`
	SelectedVideoID          string                     `json:"selected_video_id,omitempty"`
	ChannelIdentityConfirmed bool                       `json:"channel_identity_confirmed"`
	UnknownReason            LiveCheckUnknownReason     `json:"unknown_reason,omitempty"`
	Coverage                 ChannelLiveCheckCoverageV1 `json:"coverage"`
}

// VideoLiveCheckV1은 영상 player 사실 관측이다. 선택 boolean과 시각은 nil(원시 필드 없음)을
// 명시적 false/값과 구분해 보존한다.
type VideoLiveCheckV1 struct {
	VideoID                 string                   `json:"video_id"`
	ChannelID               string                   `json:"channel_id,omitempty"`
	IdentityConfirmed       bool                     `json:"identity_confirmed"`
	IsLive                  *bool                    `json:"is_live,omitempty"`
	IsLiveNow               *bool                    `json:"is_live_now,omitempty"`
	IsUpcoming              *bool                    `json:"is_upcoming,omitempty"`
	IsLiveContent           *bool                    `json:"is_live_content,omitempty"`
	IsPrivate               *bool                    `json:"is_private,omitempty"`
	HasLiveBroadcastDetails *bool                    `json:"has_live_broadcast_details,omitempty"`
	StartedAt               *time.Time               `json:"started_at,omitempty"`
	EndedAt                 *time.Time               `json:"ended_at,omitempty"`
	Availability            VideoAvailability        `json:"availability"`
	Method                  VideoAvailabilityMethod  `json:"method"`
	UnknownReason           LiveCheckUnknownReason   `json:"unknown_reason,omitempty"`
	Coverage                VideoLiveCheckCoverageV1 `json:"coverage"`
}

var (
	channelLiveCheckUnknownReasons = []LiveCheckUnknownReason{
		LiveCheckReasonIdentityMissing,
		LiveCheckReasonIdentityMismatch,
		LiveCheckReasonContradictoryFields,
		LiveCheckReasonStructureUnrecognized,
		LiveCheckReasonNotWaitingState,
		LiveCheckReasonLoginRequiredUnclassified,
		LiveCheckReasonErrorUnclassified,
		LiveCheckReasonRequestFailed,
	}
	videoLiveCheckUnknownReasons = []LiveCheckUnknownReason{
		LiveCheckReasonIdentityMissing,
		LiveCheckReasonIdentityMismatch,
		LiveCheckReasonContradictoryFields,
		LiveCheckReasonStructureUnrecognized,
		LiveCheckReasonLoginRequiredUnclassified,
		LiveCheckReasonErrorUnclassified,
		LiveCheckReasonRequestFailed,
		LiveCheckReasonAvailabilityUnclassified,
	}
	// 식별자 확인 이전에서 끝난 사유는 identity가 확인된 관측에 올 수 없다.
	identityUnconfirmedReasons = []LiveCheckUnknownReason{
		LiveCheckReasonIdentityMissing,
		LiveCheckReasonIdentityMismatch,
		LiveCheckReasonRequestFailed,
	}
	// 영상 확인에서 identity가 확인되지 않은 UNKNOWN이 가질 수 있는 첫 실패 사유다.
	videoIdentityStageReasons = []LiveCheckUnknownReason{
		LiveCheckReasonIdentityMissing,
		LiveCheckReasonIdentityMismatch,
		LiveCheckReasonStructureUnrecognized,
		LiveCheckReasonRequestFailed,
	}
)

func (k ObservationKind) isLiveCheck() bool {
	return k == KindChannelLiveCheck || k == KindVideoLiveCheck
}

// validateLiveCheckEnvelope은 라이브 확인 kind의 provider·continuity 계약을 검사한다.
// 두 kind는 YouTube.js 단독 관측이며 연속성 개념이 없다.
func validateLiveCheckEnvelope(provider Provider, kind ObservationKind, continuity Continuity) error {
	if !kind.isLiveCheck() {
		return nil
	}

	if provider != ProviderYouTubeJS {
		return fmt.Errorf("validate source observation envelope: provider %q does not support %s", provider, kind)
	}

	if continuity != ContinuityNotApplicable {
		return fmt.Errorf("validate source observation envelope: %s continuity must be %s", kind, ContinuityNotApplicable)
	}

	return nil
}

func (o ChannelLiveCheckOutcome) Valid() bool {
	switch o {
	case ChannelLiveCheckLiveVideo, ChannelLiveCheckUpcomingVideo, ChannelLiveCheckChannelPage, ChannelLiveCheckUnknown:
		return true
	default:
		return false
	}
}

func (a VideoAvailability) Valid() bool {
	switch a {
	case VideoAvailabilityPublic, VideoAvailabilityMembersOnly, VideoAvailabilityPublicUnavailable, VideoAvailabilityUnknown:
		return true
	default:
		return false
	}
}

func (a VideoAvailability) method() VideoAvailabilityMethod {
	switch a {
	case VideoAvailabilityPublic:
		return VideoAvailabilityMethodPlayerPublic
	case VideoAvailabilityMembersOnly:
		return VideoAvailabilityMethodPlayerMembersOnly
	case VideoAvailabilityPublicUnavailable:
		return VideoAvailabilityMethodPlayerPrivate
	case VideoAvailabilityUnknown:
		return VideoAvailabilityMethodUnknown
	default:
		return ""
	}
}

// NegativeLiveEvidence는 요청 채널 identity가 확인된 예정 영상·채널 페이지 연결만 공개 실시간 방송 음성으로 인정한다.
func (p *ChannelLiveCheckV1) NegativeLiveEvidence() bool {
	return p.ChannelIdentityConfirmed &&
		(p.Outcome == ChannelLiveCheckUpcomingVideo || p.Outcome == ChannelLiveCheckChannelPage)
}

// LifecycleFactsTrusted는 identity가 확인되고 수명 사실 판정 단계가 성공한 관측인지 보고한다.
// 가용성만 불명확한 availability_unclassified 외의 UNKNOWN 사유는 수명 사실을 무효화한다.
func (p *VideoLiveCheckV1) LifecycleFactsTrusted() bool {
	return p.IdentityConfirmed &&
		(p.UnknownReason == "" || p.UnknownReason == LiveCheckReasonAvailabilityUnclassified)
}

// CurrentlyLive는 신뢰 가능한 관측의 isLive=true 또는 isLiveNow=true를 현재 LIVE 사실로 본다.
func (p *VideoLiveCheckV1) CurrentlyLive() bool {
	return p.LifecycleFactsTrusted() && (isTrue(p.IsLive) || isTrue(p.IsLiveNow))
}

// VerifiedEndedAt은 identity가 맞고 현재 LIVE가 아니며 isLiveNow=false와 검증된 ended_at이 함께 있을 때만
// 종료 시각을 돌려준다. 현재 LIVE가 아닌 조건은 isLive 생략과 명시적 false 모두를 포함한다.
func (p *VideoLiveCheckV1) VerifiedEndedAt() (time.Time, bool) {
	if !p.LifecycleFactsTrusted() || p.EndedAt == nil || isTrue(p.IsLive) || p.IsLiveNow == nil || *p.IsLiveNow {
		return time.Time{}, false
	}

	return *p.EndedAt, true
}

func (c *ChannelLiveCheckCoverageV1) normalizeAndValidate(subject string) error {
	if c.ChannelID != subject {
		return errors.New("channel live check coverage channel does not match subject")
	}

	if err := validateIdentifier("channel live check coverage channel", c.ChannelID, 256); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	return nil
}

func (c *VideoLiveCheckCoverageV1) normalizeAndValidate(subject string) error {
	if c.VideoID != subject {
		return errors.New("video live check coverage video does not match subject")
	}

	if err := validateIdentifier("video live check coverage video", c.VideoID, 128); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	return nil
}

func (p *ChannelLiveCheckV1) normalizeAndValidate(subject string, completeness Completeness) error {
	if p.ChannelID != subject {
		return errors.New("channel live check channel does not match subject")
	}

	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return fmt.Errorf("normalize and validate: %w", err)
	}

	if p.SelectedVideoID != "" {
		if err := validateIdentifier("channel live check selected video", p.SelectedVideoID, 128); err != nil {
			return fmt.Errorf("validate identifier: %w", err)
		}
	}

	if !p.Outcome.Valid() {
		return fmt.Errorf("unsupported channel live check outcome %q", p.Outcome)
	}

	if p.Outcome == ChannelLiveCheckUnknown {
		if err := p.validateUnknown(completeness); err != nil {
			return fmt.Errorf("validate unknown: %w", err)
		}

		return nil
	}

	if err := p.validateKnown(completeness); err != nil {
		return fmt.Errorf("validate known: %w", err)
	}

	return nil
}

func (p *ChannelLiveCheckV1) validateKnown(completeness Completeness) error {
	if completeness != CompletenessComplete {
		return fmt.Errorf("channel live check %s requires %s completeness", p.Outcome, CompletenessComplete)
	}

	if p.UnknownReason != "" {
		return fmt.Errorf("channel live check %s must not carry an unknown reason", p.Outcome)
	}

	if !p.ChannelIdentityConfirmed {
		return fmt.Errorf("channel live check %s requires confirmed channel identity", p.Outcome)
	}

	if (p.Outcome == ChannelLiveCheckChannelPage) != (p.SelectedVideoID == "") {
		return fmt.Errorf("channel live check %s selected video does not match outcome", p.Outcome)
	}

	return nil
}

func (p *ChannelLiveCheckV1) validateUnknown(completeness Completeness) error {
	if completeness != CompletenessUnknown {
		return fmt.Errorf("channel live check %s requires %s completeness", p.Outcome, CompletenessUnknown)
	}

	if !slices.Contains(channelLiveCheckUnknownReasons, p.UnknownReason) {
		return fmt.Errorf("unsupported channel live check unknown reason %q", p.UnknownReason)
	}

	if p.ChannelIdentityConfirmed && slices.Contains(identityUnconfirmedReasons, p.UnknownReason) {
		return fmt.Errorf("channel live check reason %q cannot confirm channel identity", p.UnknownReason)
	}

	return nil
}

func (p *VideoLiveCheckV1) normalizeAndValidate(subject string, completeness Completeness, observedAt time.Time) error {
	if err := p.validateIdentity(subject); err != nil {
		return fmt.Errorf("validate identity: %w", err)
	}

	if err := p.normalizeTimes(); err != nil {
		return fmt.Errorf("normalize times: %w", err)
	}

	if err := p.validateOutcome(completeness); err != nil {
		return fmt.Errorf("validate outcome: %w", err)
	}

	if p.LifecycleFactsTrusted() {
		if err := p.validateLifecycleFacts(observedAt); err != nil {
			return fmt.Errorf("validate lifecycle facts: %w", err)
		}
	}

	if err := p.validateAvailabilityFacts(); err != nil {
		return fmt.Errorf("validate availability facts: %w", err)
	}

	return nil
}

func (p *VideoLiveCheckV1) validateIdentity(subject string) error {
	if p.VideoID != subject {
		return errors.New("video live check video does not match subject")
	}

	if err := validateIdentifier("video live check video", p.VideoID, 128); err != nil {
		return fmt.Errorf("validate identifier: %w", err)
	}

	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return fmt.Errorf("normalize and validate: %w", err)
	}

	if p.ChannelID != "" {
		if err := validateIdentifier("video live check channel", p.ChannelID, 256); err != nil {
			return fmt.Errorf("validate identifier: %w", err)
		}
	}

	if p.IdentityConfirmed && p.ChannelID == "" {
		return errors.New("video live check identity requires a channel id")
	}

	return nil
}

func (p *VideoLiveCheckV1) normalizeTimes() error {
	if err := normalizeOptionalTime(&p.StartedAt); err != nil {
		return fmt.Errorf("video live check started at: %w", err)
	}

	if err := normalizeOptionalTime(&p.EndedAt); err != nil {
		return fmt.Errorf("video live check ended at: %w", err)
	}

	return nil
}

func (p *VideoLiveCheckV1) validateOutcome(completeness Completeness) error {
	if !p.Availability.Valid() {
		return fmt.Errorf("unsupported video availability %q", p.Availability)
	}

	if p.Method != p.Availability.method() {
		return fmt.Errorf("video availability %s requires method %q", p.Availability, p.Availability.method())
	}

	if p.Availability == VideoAvailabilityUnknown {
		if err := p.validateUnknownOutcome(completeness); err != nil {
			return fmt.Errorf("validate unknown outcome: %w", err)
		}

		return nil
	}

	if completeness != CompletenessPartial || p.UnknownReason != "" || !p.IdentityConfirmed {
		return fmt.Errorf("video availability %s requires confirmed identity, %s completeness and no unknown reason", p.Availability, CompletenessPartial)
	}

	return nil
}

func (p *VideoLiveCheckV1) validateUnknownOutcome(completeness Completeness) error {
	if completeness != CompletenessUnknown {
		return fmt.Errorf("video availability %s requires %s completeness", p.Availability, CompletenessUnknown)
	}

	if !slices.Contains(videoLiveCheckUnknownReasons, p.UnknownReason) {
		return fmt.Errorf("unsupported video live check unknown reason %q", p.UnknownReason)
	}

	if p.IdentityConfirmed && slices.Contains(identityUnconfirmedReasons, p.UnknownReason) {
		return fmt.Errorf("video live check reason %q cannot confirm identity", p.UnknownReason)
	}

	if !p.IdentityConfirmed && !slices.Contains(videoIdentityStageReasons, p.UnknownReason) {
		return fmt.Errorf("video live check reason %q requires confirmed identity", p.UnknownReason)
	}

	if p.UnknownReason == LiveCheckReasonRequestFailed && p.hasResponseFacts() {
		return errors.New("video live check request failure must not carry response facts")
	}

	return nil
}

func (p *VideoLiveCheckV1) hasResponseFacts() bool {
	return p.ChannelID != "" || p.IsLive != nil || p.IsLiveNow != nil || p.IsUpcoming != nil ||
		p.IsLiveContent != nil || p.IsPrivate != nil || p.HasLiveBroadcastDetails != nil ||
		p.StartedAt != nil || p.EndedAt != nil
}

// validateLifecycleFacts는 신뢰 가능한 수명 사실이 서로 모순되지 않음을 강제한다.
// 모순된 원시 사실은 contradictory_fields UNKNOWN으로만 보존한다.
func (p *VideoLiveCheckV1) validateLifecycleFacts(observedAt time.Time) error {
	if p.IsLive != nil && p.IsLiveNow != nil && *p.IsLive != *p.IsLiveNow {
		return errors.New("video live check is_live and is_live_now disagree")
	}

	if (isTrue(p.IsLive) || isTrue(p.IsLiveNow)) && (isTrue(p.IsUpcoming) || p.EndedAt != nil) {
		return errors.New("video live check live fact coexists with upcoming or end facts")
	}

	if p.EndedAt == nil {
		return nil
	}

	if isTrue(p.IsUpcoming) {
		return errors.New("video live check upcoming fact coexists with end facts")
	}

	if p.StartedAt != nil && p.EndedAt.Before(*p.StartedAt) {
		return errors.New("video live check ended at precedes started at")
	}

	if observedAt.IsZero() || p.EndedAt.After(observedAt) {
		return errors.New("video live check ended at is after observed at")
	}

	return nil
}

// validateAvailabilityFacts는 가용성 분류가 원시 isPrivate 값으로만 성립하게 한다.
// 필드가 없는 응답에 기본값을 두지 않는다.
func (p *VideoLiveCheckV1) validateAvailabilityFacts() error {
	switch p.Availability {
	case VideoAvailabilityPublic:
		if p.IsPrivate == nil || *p.IsPrivate {
			return errors.New("video availability PUBLIC requires raw is_private=false")
		}
	case VideoAvailabilityPublicUnavailable:
		if !isTrue(p.IsPrivate) {
			return errors.New("video availability PUBLIC_UNAVAILABLE requires raw is_private=true")
		}
	case VideoAvailabilityMembersOnly, VideoAvailabilityUnknown:
	}

	return nil
}

func isTrue(value *bool) bool {
	return value != nil && *value
}
