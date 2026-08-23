package htmlscraper

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (s *Service) decodeOfficialScheduleAPI(body []byte) ([]*domain.Stream, officialScheduleRowStats, error) {
	groups, err := decodeOfficialScheduleGroups(body)
	if err != nil {
		return nil, officialScheduleRowStats{}, err
	}
	rows, err := decodeOfficialScheduleRows(groups)
	if err != nil {
		return nil, officialScheduleRowStats{}, err
	}
	return s.mapOfficialScheduleRows(rows)
}

func decodeOfficialScheduleGroups(body []byte) ([]jsontext.Value, error) {
	trimmed := bytes.TrimSpace(body)
	if !jsontext.Value(trimmed).IsValid() {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonDecode, 0, fmt.Errorf("decode official schedule response: invalid JSON"))
	}
	if len(trimmed) == 0 || trimmed[0] != 0x7b {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("official schedule response root must be an object"))
	}

	var root map[string]jsontext.Value
	if err := jsonv2.Unmarshal(trimmed, &root); err != nil {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonDecode, 0, fmt.Errorf("decode official schedule response: %w", err))
	}
	rawGroups, ok := root["dateGroupList"]
	if !ok {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("official schedule response missing dateGroupList"))
	}
	groups, err := decodeRawJSONArray(rawGroups, "dateGroupList")
	if err != nil {
		return nil, err
	}
	return groups, nil
}

func decodeOfficialScheduleRows(groups []jsontext.Value) ([]jsontext.Value, error) {
	rows := make([]jsontext.Value, 0)
	for index, rawGroup := range groups {
		var group map[string]jsontext.Value
		if err := jsonv2.Unmarshal(rawGroup, &group); err != nil {
			return nil, newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("decode official schedule group %d: %w", index, err))
		}
		rawRows, ok := group["videoList"]
		if !ok {
			return nil, newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("official schedule group %d missing videoList", index))
		}
		groupRows, err := decodeRawJSONArray(rawRows, fmt.Sprintf("dateGroupList[%d].videoList", index))
		if err != nil {
			return nil, err
		}
		rows = append(rows, groupRows...)
	}
	return rows, nil
}

func decodeRawJSONArray(raw jsontext.Value, field string) ([]jsontext.Value, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("official schedule field %s must be an array", field))
	}

	var values []jsontext.Value
	if err := jsonv2.Unmarshal(trimmed, &values); err != nil {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("decode official schedule field %s: %w", field, err))
	}
	return values, nil
}
