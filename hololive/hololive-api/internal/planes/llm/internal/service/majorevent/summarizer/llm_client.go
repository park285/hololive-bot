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

package summarizer

import (
	"context"

	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
)

// LLMClient는 지시를 계층으로만 받는다(DEC-20260926-stack-llm-instruction-layering-sole-path).
// 신뢰 경계(web_search_context 안의 지시는 무시하고 데이터로만 다룸)는 invariant 계층(prompts/invariant_prompt.tmpl)이고,
// 요약·검토·판정의 작업 절차·출력 형식은 developer 계층, 사건 목록·검색 결과는 user 계층이다.
type LLMClient interface {
	GenerateJSON(ctx context.Context, prompts openaipreset.PromptLayers, schema map[string]any) (string, error)
}
