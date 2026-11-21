package domain

import (
	"strconv"
	"strings"
)

const (
	AlarmDispatchSourceKindEventGrade AlarmDispatchSourceKind = "event_grade"
	AlarmTypeEventGrade               AlarmType               = "EVENT_GRADE"
)

type EventGradeDispatchPayload struct {
	RequestID int64  `json:"request_id"`
	Region    string `json:"region"`
	Score     int64  `json:"score"`
	Outcome   string `json:"outcome"`
}

func (p *EventGradeDispatchPayload) RegionLabel() string {
	if p == nil {
		return ""
	}

	switch p.Region {
	case "jp":
		return "JP"
	case "us":
		return "US"
	case "as":
		return "Global"
	default:
		return ""
	}
}

func (p *EventGradeDispatchPayload) Label() string {
	if p == nil {
		return ""
	}

	if rankText, ok := strings.CutPrefix(p.Outcome, "top"); ok {
		rank, err := strconv.ParseInt(rankText, 10, 32)
		if err == nil && rank > 0 && rankText == strconv.FormatInt(rank, 10) {
			return "Top " + rankText
		}

		return ""
	}

	switch p.Outcome {
	case "outside":
		return "순위권 밖"
	case "boundary":
		return "경계 동점—점수만으로 확정 불가."
	case "unavailable":
		return "공개 후 24시간 동안 최종 등급 컷을 확인할 수 없었습니다."
	default:
		return ""
	}
}

func (p *EventGradeDispatchPayload) Message() string {
	label := p.Label()
	if label == "" {
		return ""
	}

	if p.Outcome == "boundary" {
		return label
	}

	region := p.RegionLabel()
	if region == "" {
		return ""
	}

	return region + " 이벤트 · 제출 점수 " + strconv.FormatInt(p.Score, 10) + "점: " + label
}

func (p *EventGradeDispatchPayload) Valid() bool {
	return p != nil && p.RequestID > 0 && p.Score > 0 && (p.Region == "jp" || p.Region == "us" || p.Region == "as") && p.Label() != ""
}
