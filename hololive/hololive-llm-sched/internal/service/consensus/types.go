// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package consensus

import (
	"strings"
	"time"
)

type ReviewIssue struct {
	Field       string `json:"field"`
	ItemIndex   int    `json:"item_index"`
	Severity    string `json:"severity"` // critical, warning, info
	Description string `json:"description"`
}

type ReviewVerdict struct {
	Approved   bool          `json:"approved"`
	Issues     []ReviewIssue `json:"issues"`
	Confidence float64       `json:"confidence"` // 0.0 ~ 1.0
}

type Config struct {
	ConfidenceThreshold float64       // 기본 0.85, 범위 0.0~1.0
	ReviewTimeout       time.Duration // 기본 30s, 최소 5s
	AdjudicateTimeout   time.Duration // 기본 45s, 최소 5s
}

type FinalOutputReviewResponse struct {
	Summary string `json:"summary"`
}

func NormalizeSeverity(s string) string {
	normalized := strings.TrimSpace(strings.ToLower(s))
	switch normalized {
	case "critical", "warning", "info":
		return normalized
	default:
		return "info"
	}
}

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

func HasCriticalIssues(issues []ReviewIssue) bool {
	for i := range issues {
		if issues[i].Severity == "critical" {
			return true
		}
	}
	return false
}
