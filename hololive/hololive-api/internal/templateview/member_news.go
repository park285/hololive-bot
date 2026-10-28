package templateview

import (
	"slices"
	"strings"

	membernewsmodel "github.com/kapu/hololive-api/internal/planes/llm/membernews/model"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type MemberNewsDigestData struct {
	Headline     string
	TopItems     []membernewsmodel.SummaryItem
	MoreSummary  string
	TotalCount   int
	DisplayCount int
}

// BuildMemberNewsDigest는 입력을 보존하며 직접 조회와 예약 뉴스의 표시 데이터를 만든다.
func BuildMemberNewsDigest(digest membernewsmodel.Digest, store *messagestrings.Store) MemberNewsDigestData {
	items := slices.Clone(digest.TopItems)
	for i := range items {
		if label, ok := store.Lookup(messagestrings.NamespaceNewsCat, strings.ToLower(strings.TrimSpace(items[i].Category))); ok {
			items[i].Category = label
		}
	}

	count := len(items)
	// 템플릿이 출력하는 추가 요약은 줄 수와 무관하게 한 블록이다.
	if digest.MoreSummary != "" {
		count++
	}

	return MemberNewsDigestData{
		Headline: digest.Headline, TopItems: items, MoreSummary: digest.MoreSummary,
		TotalCount: digest.TotalCount, DisplayCount: count,
	}
}
