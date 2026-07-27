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

package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
)

func TestRenderOrError_SeeMoreFoldToggle(t *testing.T) {
	t.Parallel()

	longBody := "다이제스트 헤더\n" + strings.Repeat("뉴스 항목 행입니다\n", 40)
	renderer := setupFormatterRenderer(t, domain.TemplateKeyCmdMemberNewsDigest, longBody)

	on := newLLMSchedulerFormatter("!", renderer, nil, true)
	folded := on.renderOrError(context.Background(), domain.TemplateKeyCmdMemberNewsDigest, nil, "warn")
	assert.True(t, strings.HasPrefix(folded, "다이제스트 헤더\n"))
	assert.Contains(t, folded, "\u200b")

	off := newLLMSchedulerFormatter("!", renderer, nil, false)
	assert.NotContains(t, off.renderOrError(context.Background(), domain.TemplateKeyCmdMemberNewsDigest, nil, "warn"), "\u200b")
}
