package sourceobservation

import (
	"errors"
	"fmt"
	"time"
)

// payloadDecodeInput은 kind별 decoder가 payload를 검증할 때 쓰는 envelope 문맥이다.
type payloadDecodeInput struct {
	subjectKey         string
	completeness       Completeness
	contractGeneration int64
	observedAt         time.Time
}

type payloadDecoder func(raw []byte, input payloadDecodeInput) (any, any, error)

var payloadDecoders = map[ObservationKind]payloadDecoder{
	KindCommunityPage:    decodeCommunityPayload,
	KindVideoList:        decodeVideoListPayload,
	KindShortsList:       decodeShortsListPayload,
	KindLiveSnapshot:     decodeLiveSnapshotPayload,
	KindViewerSample:     decodeViewerSamplePayload,
	KindChannelStats:     decodeChannelStatsPayload,
	KindChannelProfile:   decodeChannelProfilePayload,
	KindChannelPhoto:     decodeChannelPhotoPayload,
	KindSchedule:         decodeSchedulePayload,
	KindChannelLiveCheck: decodeChannelLiveCheckPayload,
	KindVideoLiveCheck:   decodeVideoLiveCheckPayload,
}

func canonicalPayloadAndScope(envelope *Envelope) (payloadJSON, coverageJSON []byte, err error) {
	raw := envelope.Payload
	if len(raw) == 0 || len(raw) > MaxPayloadBytes {
		return nil, nil, errors.New("payload size is outside the accepted range")
	}

	decode, ok := payloadDecoders[envelope.ObservationKind]
	if !ok {
		return nil, nil, fmt.Errorf("unsupported observation kind %q", envelope.ObservationKind)
	}

	payload, coverage, err := decode(raw, payloadDecodeInput{
		subjectKey:         envelope.SubjectKey,
		completeness:       envelope.Completeness,
		contractGeneration: envelope.ContractGeneration,
		observedAt:         envelope.ObservedAt,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("decode: %w", err)
	}

	out1, out2, err := canonicalizePayloadAndScope(payload, coverage)
	if err != nil {
		return out1, out2, fmt.Errorf("canonicalize payload and scope: %w", err)
	}

	return out1, out2, nil
}

func canonicalizePayloadAndScope(payload, coverage any) (payloadJSON, coverageJSON []byte, err error) {
	canonicalPayload, err := canonicalJSON(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("canonicalize payload: %w", err)
	}

	canonicalScope, err := canonicalJSON(coverage)
	if err != nil {
		return nil, nil, fmt.Errorf("canonicalize coverage: %w", err)
	}

	return canonicalPayload, canonicalScope, nil
}

func decodeCommunityPayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
	value := CommunityPayloadV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode community payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	if err := validatePaginatedCompleteness(KindCommunityPage, input.completeness, value.Coverage.Exhausted); err != nil {
		return nil, nil, fmt.Errorf("validate paginated completeness: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeVideoListPayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
	value := VideoListV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode video list payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	if err := validatePaginatedCompleteness(KindVideoList, input.completeness, value.Coverage.Exhausted); err != nil {
		return nil, nil, fmt.Errorf("validate paginated completeness: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeShortsListPayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
	value := ShortsListV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode shorts list payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	if err := validatePaginatedCompleteness(KindShortsList, input.completeness, value.Coverage.Exhausted); err != nil {
		return nil, nil, fmt.Errorf("validate paginated completeness: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeLiveSnapshotPayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
	if input.contractGeneration == 1 {
		legacyPayload, legacyCoverage, err := decodeLiveSnapshotGeneration1(raw, input.subjectKey)
		if err != nil {
			return nil, nil, fmt.Errorf("decode live snapshot generation one: %w", err)
		}

		return legacyPayload, legacyCoverage, nil
	}

	if input.contractGeneration != LiveSnapshotMetadataContractGeneration {
		return nil, nil, fmt.Errorf("unsupported live snapshot contract generation %d", input.contractGeneration)
	}

	value := LiveSnapshotV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode live snapshot payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

type liveSessionGeneration1 struct {
	VideoID     string     `json:"video_id"`
	ChannelID   string     `json:"channel_id"`
	Status      string     `json:"status"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
}

type liveSnapshotGeneration1 struct {
	Sessions []liveSessionGeneration1 `json:"sessions"`
	Coverage GlobalChannelCoverageV1  `json:"coverage"`
}

func decodeLiveSnapshotGeneration1(raw []byte, subjectKey string) (payload, coverage any, err error) {
	legacy := liveSnapshotGeneration1{}
	if err := decodeStrictJSON(raw, &legacy); err != nil {
		return nil, nil, fmt.Errorf("decode live snapshot payload: %w", err)
	}

	value := LiveSnapshotV1{
		Sessions: make([]LiveSessionV1, len(legacy.Sessions)),
		Coverage: legacy.Coverage,
	}
	for i := range legacy.Sessions {
		value.Sessions[i] = LiveSessionV1{
			VideoID:     legacy.Sessions[i].VideoID,
			ChannelID:   legacy.Sessions[i].ChannelID,
			Status:      legacy.Sessions[i].Status,
			ScheduledAt: legacy.Sessions[i].ScheduledAt,
			StartedAt:   legacy.Sessions[i].StartedAt,
			EndedAt:     legacy.Sessions[i].EndedAt,
		}
	}

	if err := value.normalizeAndValidate(subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	legacy.Coverage = value.Coverage
	legacy.Sessions = make([]liveSessionGeneration1, len(value.Sessions))

	for i := range value.Sessions {
		legacy.Sessions[i] = liveSessionGeneration1{
			VideoID:     value.Sessions[i].VideoID,
			ChannelID:   value.Sessions[i].ChannelID,
			Status:      value.Sessions[i].Status,
			ScheduledAt: value.Sessions[i].ScheduledAt,
			StartedAt:   value.Sessions[i].StartedAt,
			EndedAt:     value.Sessions[i].EndedAt,
		}
	}

	return legacy, legacy.Coverage, nil
}

func decodeViewerSamplePayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
	value := ViewerSampleV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode viewer sample payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeChannelStatsPayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
	value := ChannelStatsV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode channel stats payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeChannelProfilePayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
	value := ChannelProfileV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode channel profile payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeChannelPhotoPayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
	value := ChannelPhotoV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode channel photo payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeSchedulePayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
	value := ScheduleSnapshotV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode schedule payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeChannelLiveCheckPayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
	if input.contractGeneration != LiveCheckContractGeneration {
		return nil, nil, fmt.Errorf("unsupported channel live check contract generation %d", input.contractGeneration)
	}

	value := ChannelLiveCheckV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode channel live check payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey, input.completeness); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeVideoLiveCheckPayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
	if input.contractGeneration != LiveCheckContractGeneration {
		return nil, nil, fmt.Errorf("unsupported video live check contract generation %d", input.contractGeneration)
	}

	value := VideoLiveCheckV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode video live check payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey, input.completeness, input.observedAt); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

func validatePaginatedCompleteness(kind ObservationKind, completeness Completeness, exhausted bool) error {
	if completeness == CompletenessComplete && !exhausted {
		return fmt.Errorf("%s payload cannot be COMPLETE when coverage is not exhausted", kind)
	}

	return nil
}
