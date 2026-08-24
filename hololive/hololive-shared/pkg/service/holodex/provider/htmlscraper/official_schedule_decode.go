package htmlscraper

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (s *Service) decodeOfficialScheduleAPI(body []byte) ([]*domain.Stream, officialScheduleRowStats, error) {
	groups, err := decodeOfficialScheduleGroups(body)
	if err != nil {
		return nil, officialScheduleRowStats{}, fmt.Errorf("decode official schedule groups: %w", err)
	}

	rows, err := decodeOfficialScheduleRows(groups)
	if err != nil {
		return nil, officialScheduleRowStats{}, fmt.Errorf("decode official schedule rows: %w", err)
	}

	out1, out2, err := s.mapOfficialScheduleRows(rows)
	if err != nil {
		return out1, out2, fmt.Errorf("map official schedule rows: %w", err)
	}

	return out1, out2, nil
}

func decodeOfficialScheduleGroups(body []byte) ([]jsontext.Value, error) {
	trimmed := bytes.TrimSpace(body)
	if !jsontext.Value(trimmed).IsValid() {
		return nil, errors.Join(officialScheduleDecodeError(officialScheduleReasonDecode, errors.New("decode official schedule response: invalid JSON")))
	}

	if len(trimmed) == 0 || trimmed[0] != 0x7b {
		return nil, errors.Join(officialScheduleDecodeError(officialScheduleReasonSchema, errors.New("official schedule response root must be an object")))
	}

	var root map[string]jsontext.Value

	if err := jsonv2.Unmarshal(trimmed, &root); err != nil {
		return nil, errors.Join(officialScheduleDecodeError(officialScheduleReasonDecode, fmt.Errorf("decode official schedule response: %w", err)))
	}

	rawGroups, ok := root["dateGroupList"]
	if !ok {
		return nil, errors.Join(officialScheduleDecodeError(officialScheduleReasonSchema, errors.New("official schedule response missing dateGroupList")))
	}

	groups, err := decodeRawJSONArray(rawGroups, "dateGroupList")
	if err != nil {
		return nil, fmt.Errorf("decode raw JSON array: %w", err)
	}

	return groups, nil
}

func officialScheduleDecodeError(reason officialScheduleReason, cause error) error {
	err := newOfficialScheduleSourceError(reason, 0, cause)
	if err != nil {
		return fmt.Errorf("official schedule source error: %w", err)
	}

	return nil
}

func decodeOfficialScheduleRows(groups []jsontext.Value) ([]jsontext.Value, error) {
	rows := make([]jsontext.Value, 0)

	for index, rawGroup := range groups {
		groupRows, err := decodeOfficialScheduleGroup(rawGroup, index)
		if err != nil {
			return nil, fmt.Errorf("decode official schedule group: %w", err)
		}

		rows = append(rows, groupRows...)
	}

	return rows, nil
}

func decodeOfficialScheduleGroup(rawGroup jsontext.Value, index int) ([]jsontext.Value, error) {
	var group map[string]jsontext.Value

	if err := jsonv2.Unmarshal(rawGroup, &group); err != nil {
		return nil, errors.Join(officialScheduleDecodeError(officialScheduleReasonSchema, fmt.Errorf("decode official schedule group %d: %w", index, err)))
	}

	rawRows, ok := group["videoList"]
	if !ok {
		return nil, errors.Join(officialScheduleDecodeError(officialScheduleReasonSchema, fmt.Errorf("official schedule group %d missing videoList", index)))
	}

	groupRows, err := decodeRawJSONArray(rawRows, fmt.Sprintf("dateGroupList[%d].videoList", index))
	if err != nil {
		return nil, fmt.Errorf("decode raw JSON array: %w", err)
	}

	return groupRows, nil
}

func decodeRawJSONArray(raw jsontext.Value, field string) ([]jsontext.Value, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		if err := newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("official schedule field %s must be an array", field)); err != nil {
			return nil, fmt.Errorf("official schedule source error: %w", err)
		}

		return nil, nil
	}

	var values []jsontext.Value

	if err := jsonv2.Unmarshal(trimmed, &values); err != nil {
		if newOfficialScheduleErr := newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("decode official schedule field %s: %w", field, err)); newOfficialScheduleErr != nil {
			return nil, fmt.Errorf("official schedule source error: %w", newOfficialScheduleErr)
		}

		return nil, nil
	}

	return values, nil
}
