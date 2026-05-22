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

package filter

import (
	"strings"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
)

var categoryPriority = map[model.Category]int{
	model.CategoryBirthdayLive: 0,
	model.CategorySoloLive:     1,
	model.CategoryCollab:       2,
	model.CategoryEvent:        3,
	model.CategoryGoods:        4,
	model.CategoryOther:        5,
}

func classifyCategory(candidate model.Candidate) model.Category {
	text := strings.ToLower(candidate.Title + " " + candidate.Description)

	keywordRules := []struct {
		category model.Category
		keywords []string
	}{
		{category: model.CategoryBirthdayLive, keywords: []string{"生誕", "생일", "birthday"}},
		{category: model.CategorySoloLive, keywords: []string{"ソロライブ", "solo live", "단독 라이브"}},
		{category: model.CategoryCollab, keywords: []string{"コラボ", "콜라보", "collaboration"}},
		{category: model.CategoryGoods, keywords: []string{"グッズ", "굿즈", "merchandise"}},
		{category: model.CategoryEvent, keywords: []string{"fes", "expo", "live", "concert", "event"}},
	}

	for _, rule := range keywordRules {
		if containsAny(text, rule.keywords) {
			return rule.category
		}
	}

	return model.CategoryOther
}

func containsAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}
