// Package model: 서비스 간 공유 도메인 모델
package model

import "context"

// SummaryResultType: 요약/폴백 결과 출처 구분.
type SummaryResultType string

const (
	SummaryResultPrimary  SummaryResultType = "primary"
	SummaryResultFallback SummaryResultType = "fallback"
	SummaryResultEmpty    SummaryResultType = "empty"
)

// SearchResult: 웹 검색 결과 단일 항목
type SearchResult struct {
	Title         string
	URL           string
	Content       string
	PublishedDate string
}

// WebSearcher: 외부 검색 추상화 인터페이스
type WebSearcher interface {
	Search(ctx context.Context, query string) ([]SearchResult, error)
}
