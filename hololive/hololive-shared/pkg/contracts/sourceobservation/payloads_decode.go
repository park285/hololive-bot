package sourceobservation

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// payloadDecodeInput은 kind별 decoder가 payload를 검증할 때 쓰는 envelope 문맥이다.
type payloadDecodeInput struct {
	subjectKey         string
	completeness       Completeness
	contractGeneration int64
	observedAt         time.Time
	schemaVersion      int16
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
		schemaVersion:      envelope.SchemaVersion,
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
	if input.contractGeneration != VideoListPublicationContractGeneration {
		return nil, nil, fmt.Errorf("unsupported video list contract generation %d", input.contractGeneration)
	}

	value := VideoListV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode video list payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	for i := range value.Videos {
		if err := validatePublicationItem(&value.Videos[i], input.observedAt); err != nil {
			return nil, nil, fmt.Errorf("validate video publication: %w", err)
		}
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

	for i := range value.Videos {
		if value.Videos[i].Publication != nil {
			return nil, nil, errors.New("shorts list must not carry video publication evidence")
		}
	}

	if err := validatePaginatedCompleteness(KindShortsList, input.completeness, value.Coverage.Exhausted); err != nil {
		return nil, nil, fmt.Errorf("validate paginated completeness: %w", err)
	}

	return value, value.Coverage, nil
}

// generation 2는 과거 coverage 의미를 보존하고 generation 3는 streams 조회 증명을 요구한다.
// 퇴역 generation 1 decoder는 기존 제거 조건을 충족해 삭제된 상태를 유지한다.
func decodeLiveSnapshotPayload(raw []byte, input payloadDecodeInput) (payload, coverage any, err error) {
	if input.contractGeneration != LiveSnapshotMetadataContractGeneration && input.contractGeneration != LiveSnapshotQueryContractGeneration {
		return nil, nil, fmt.Errorf("unsupported live snapshot contract generation %d", input.contractGeneration)
	}

	value := LiveSnapshotV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode live snapshot payload: %w", err)
	}

	if err := value.normalizeAndValidate(input.subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	if err := validateLiveSnapshotQueryProof(&value, input); err != nil {
		return nil, nil, err
	}

	return value, value.Coverage, nil
}

func validateLiveSnapshotQueryProof(value *LiveSnapshotV1, input payloadDecodeInput) error {
	if input.contractGeneration == LiveSnapshotMetadataContractGeneration && value.Query != nil {
		return errors.New("legacy live snapshot cannot carry query proof")
	}

	if input.contractGeneration == LiveSnapshotQueryContractGeneration {
		query := value.Query
		if query == nil || query.ChannelID != input.subjectKey || query.Source != "streams" || query.PageCount != 1 {
			return errors.New("live snapshot query proof is missing or invalid")
		}

		query.Statuses = slices.Clone(query.Statuses)
		slices.Sort(query.Statuses)

		if !slices.Equal(query.Statuses, []string{"ENDED", "LIVE", "UPCOMING"}) || !slices.Equal(query.Statuses, value.Coverage.Filters.Statuses) {
			return errors.New("live snapshot coverage does not match streams query scope")
		}

		if input.completeness == CompletenessComplete && (!query.Exhausted || query.AccessRestricted) {
			return errors.New("limited live snapshot cannot provide complete negative coverage")
		}
	}

	return nil
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
	if input.contractGeneration != VideoLifecycleContractGeneration {
		return nil, nil, fmt.Errorf("unsupported video live check contract generation %d", input.contractGeneration)
	}

	value := VideoLiveCheckV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode video live check payload: %w", err)
	}

	if input.schemaVersion != VideoLifecycleSchemaVersion {
		return nil, nil, errors.New("video lifecycle generation requires schema 2")
	}

	if value.LifecycleFactsTrusted() {
		if err := validateVideoLifecycleV2(&value, input.observedAt); err != nil {
			return nil, nil, fmt.Errorf("validate video lifecycle: %w", err)
		}
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
