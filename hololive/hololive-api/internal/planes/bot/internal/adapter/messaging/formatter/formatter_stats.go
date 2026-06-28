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

package formatter

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/stringutil"
)

type statsCountTemplateData struct {
	MemberName  string
	Subscribers string
}

type statsGainerView struct {
	Rank       int
	MemberName string
	Delta      string
	Current    string
}

type statsGainersTemplateData struct {
	Period  string
	Gainers []statsGainerView
}

func (f *ResponseFormatter) FormatSubscriberCount(ctx context.Context, memberName string, subscribers uint64) string {
	data := statsCountTemplateData{
		MemberName:  memberName,
		Subscribers: util.FormatKoreanNumber(uint64ToInt64(subscribers)),
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdStatsCount, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func (f *ResponseFormatter) FormatStatsTopGainers(ctx context.Context, periodLabel string, gainers []domain.RankEntry) string {
	data := statsGainersTemplateData{
		Period:  stringutil.TrimSpace(periodLabel),
		Gainers: statsGainerViews(gainers),
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdStatsGainers, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func statsGainerViews(gainers []domain.RankEntry) []statsGainerView {
	if len(gainers) == 0 {
		return nil
	}

	views := make([]statsGainerView, len(gainers))
	for i, entry := range gainers {
		view := statsGainerView{
			Rank:       entry.Rank,
			MemberName: entry.MemberName,
			Delta:      util.FormatKoreanNumber(entry.Value),
		}

		if entry.CurrentSubscribers > 0 {
			view.Current = util.FormatKoreanNumber(uint64ToInt64(entry.CurrentSubscribers))
		}

		views[i] = view
	}

	return views
}

func uint64ToInt64(value uint64) int64 {
	const maxInt64 = uint64(1<<63 - 1)
	if value > maxInt64 {
		return int64(maxInt64)
	}
	return int64(value)
}
