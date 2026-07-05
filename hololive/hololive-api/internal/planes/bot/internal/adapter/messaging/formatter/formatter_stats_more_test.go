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
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/stretchr/testify/assert"
)

const cmdStatsCountBody = `📊 {{.MemberName}} 구독자 {{.Subscribers}}명`

func TestFormatSubscriberCount(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdStatsCount: cmdStatsCountBody,
	})
	formatter := NewResponseFormatter("!", renderer)

	assert.Equal(t,
		"📊 호시마치 스이세이 구독자 205만명",
		formatter.FormatSubscriberCount(t.Context(), "호시마치 스이세이", 2050000))
}

func TestFormatSubscriberCount_Fallback(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", setupFormatterTestRenderer(t, map[domain.TemplateKey]string{}))

	assert.Equal(t,
		messagestrings.FallbackSentinel,
		formatter.FormatSubscriberCount(t.Context(), "호시마치 스이세이", 2050000))
}
