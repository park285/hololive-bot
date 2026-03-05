// Package consensus: LLM consensus 파이프라인 공통 타입
package consensus

import (
	"strings"
	"time"
)

// ReviewIssue: 리뷰어가 발견한 개별 이슈
type ReviewIssue struct {
	Field       string `json:"field"`
	ItemIndex   int    `json:"item_index"`
	Severity    string `json:"severity"` // critical, warning, info
	Description string `json:"description"`
}

// ReviewVerdict: 리뷰어의 검증 결과
type ReviewVerdict struct {
	Approved   bool          `json:"approved"`
	Issues     []ReviewIssue `json:"issues"`
	Confidence float64       `json:"confidence"` // 0.0 ~ 1.0
}

// Config: consensus 파이프라인 설정
type Config struct {
	ConfidenceThreshold float64       // 기본 0.85, 범위 0.0~1.0
	ReviewTimeout       time.Duration // 기본 30s, 최소 5s
	AdjudicateTimeout   time.Duration // 기본 45s, 최소 5s
}

// FinalOutputReviewResponse: 최종 출력 리뷰 응답
type FinalOutputReviewResponse struct {
	Summary string `json:"summary"`
}

// NormalizeSeverity: 유효하지 않은 severity를 info로 정규화
func NormalizeSeverity(s string) string {
	normalized := strings.TrimSpace(strings.ToLower(s))
	switch normalized {
	case "critical", "warning", "info":
		return normalized
	default:
		return "info"
	}
}

// NeedsAdjudication: 리뷰 결과에 따라 adjudication 필요 여부 판정
func NeedsAdjudication(verdict *ReviewVerdict, confidenceThreshold float64) bool {
	if verdict == nil {
		return false
	}
	if !verdict.Approved {
		return true
	}
	if HasCriticalIssues(verdict.Issues) {
		return true
	}
	return verdict.Confidence < confidenceThreshold
}

// HasCriticalIssues: critical severity 존재 여부
func HasCriticalIssues(issues []ReviewIssue) bool {
	for i := range issues {
		if issues[i].Severity == "critical" {
			return true
		}
	}
	return false
}
