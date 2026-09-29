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

// decodeLiveSnapshotPayload는 세션 메타데이터를 담는 contract generation 2만 받는다. 이전 generation 1(identity/status/time만)
// decoder는 runbook 제거 조건(youtube-collector.md "Live metadata contract", 활성화 4단계: generation 1 queue가 비고
// replay 필요가 없음)이 T18(2026-09-26)에서 current generation 2·미처리 generation 1 관측 0건으로 충족되어 지웠다(계획
// T11 C6, stack-audit 2026-09-26 holo-sourceobservation-live-snapshot-gen1). 남은 generation 1 관측은 unsupported로 드러난다.
func decodeLiveSnapshotPayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
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
