package sourceobservation

import (
	"errors"
	"fmt"
)

type payloadDecoder func(raw []byte, subjectKey string, completeness Completeness) (any, any, error)

var payloadDecoders = map[ObservationKind]payloadDecoder{
	KindCommunityPage:  decodeCommunityPayload,
	KindVideoList:      decodeVideoListPayload,
	KindShortsList:     decodeShortsListPayload,
	KindLiveSnapshot:   decodeLiveSnapshotPayload,
	KindViewerSample:   decodeViewerSamplePayload,
	KindChannelStats:   decodeChannelStatsPayload,
	KindChannelProfile: decodeChannelProfilePayload,
	KindChannelPhoto:   decodeChannelPhotoPayload,
	KindSchedule:       decodeSchedulePayload,
}

func canonicalPayloadAndScope(kind ObservationKind, subjectKey string, completeness Completeness, raw []byte) (payloadJSON, coverageJSON []byte, err error) {
	if len(raw) == 0 || len(raw) > MaxPayloadBytes {
		return nil, nil, errors.New("payload size is outside the accepted range")
	}

	decode, ok := payloadDecoders[kind]
	if !ok {
		return nil, nil, fmt.Errorf("unsupported observation kind %q", kind)
	}

	payload, coverage, err := decode(raw, subjectKey, completeness)
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

func decodeCommunityPayload(raw []byte, subjectKey string, completeness Completeness) (payload, coverage any, err error) {
	value := CommunityPayloadV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode community payload: %w", err)
	}

	if err := value.normalizeAndValidate(subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	if err := validatePaginatedCompleteness(KindCommunityPage, completeness, value.Coverage.Exhausted); err != nil {
		return nil, nil, fmt.Errorf("validate paginated completeness: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeVideoListPayload(raw []byte, subjectKey string, completeness Completeness) (payload, coverage any, err error) {
	value := VideoListV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode video list payload: %w", err)
	}

	if err := value.normalizeAndValidate(subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	if err := validatePaginatedCompleteness(KindVideoList, completeness, value.Coverage.Exhausted); err != nil {
		return nil, nil, fmt.Errorf("validate paginated completeness: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeShortsListPayload(raw []byte, subjectKey string, completeness Completeness) (payload, coverage any, err error) {
	value := ShortsListV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode shorts list payload: %w", err)
	}

	if err := value.normalizeAndValidate(subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	if err := validatePaginatedCompleteness(KindShortsList, completeness, value.Coverage.Exhausted); err != nil {
		return nil, nil, fmt.Errorf("validate paginated completeness: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeLiveSnapshotPayload(raw []byte, subjectKey string, _ Completeness) (payload, coverage any, err error) {
	value := LiveSnapshotV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode live snapshot payload: %w", err)
	}

	if err := value.normalizeAndValidate(subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeViewerSamplePayload(raw []byte, subjectKey string, _ Completeness) (payload, coverage any, err error) {
	value := ViewerSampleV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode viewer sample payload: %w", err)
	}

	if err := value.normalizeAndValidate(subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeChannelStatsPayload(raw []byte, subjectKey string, _ Completeness) (payload, coverage any, err error) {
	value := ChannelStatsV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode channel stats payload: %w", err)
	}

	if err := value.normalizeAndValidate(subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeChannelProfilePayload(raw []byte, subjectKey string, _ Completeness) (payload, coverage any, err error) {
	value := ChannelProfileV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode channel profile payload: %w", err)
	}

	if err := value.normalizeAndValidate(subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeChannelPhotoPayload(raw []byte, subjectKey string, _ Completeness) (payload, coverage any, err error) {
	value := ChannelPhotoV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode channel photo payload: %w", err)
	}

	if err := value.normalizeAndValidate(subjectKey); err != nil {
		return nil, nil, fmt.Errorf("normalize and validate: %w", err)
	}

	return value, value.Coverage, nil
}

func decodeSchedulePayload(raw []byte, subjectKey string, _ Completeness) (payload, coverage any, err error) {
	value := ScheduleSnapshotV1{}
	if err := decodeStrictJSON(raw, &value); err != nil {
		return nil, nil, fmt.Errorf("decode schedule payload: %w", err)
	}

	if err := value.normalizeAndValidate(subjectKey); err != nil {
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
