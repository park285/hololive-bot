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
	"fmt"
	"strings"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/templateview"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type ResponseFormatter struct {
	prefix         string
	renderer       *template.Renderer
	messageStrings *messagestrings.Store
	seeMoreFold    bool
}

type Option func(*ResponseFormatter)

func WithMessageStrings(store *messagestrings.Store) Option {
	return func(f *ResponseFormatter) { f.messageStrings = store }
}

func WithSeeMoreFold(enabled bool) Option {
	return func(f *ResponseFormatter) { f.seeMoreFold = enabled }
}

func (f *ResponseFormatter) foldSeeMore(s string, foldEligible bool) string {
	if f == nil || !f.seeMoreFold || !foldEligible {
		return s
	}

	return templateview.FoldForSeeMore(s)
}

// renderResponse는 조회 렌더 실패 문구를 펼친 채 유지하고, 성공 본문에만 접기 정책을 적용한다.
func (f *ResponseFormatter) renderResponse(ctx context.Context, key domain.TemplateKey, data any, foldEligible bool) string {
	rendered, err := f.render(ctx, key, data)
	if err != nil {
		return f.renderFailureText()
	}

	return f.foldSeeMore(rendered, foldEligible)
}

func (f *ResponseFormatter) render(ctx context.Context, key domain.TemplateKey, data any) (string, error) {
	if f == nil || f.renderer == nil {
		return "", fmt.Errorf("render template %s: renderer not configured", key)
	}

	rendered, err := f.renderer.Render(ctx, key, "", data)
	if err != nil {
		return "", fmt.Errorf("render template %s: %w", key, err)
	}

	return strings.TrimRight(rendered, "\n"), nil
}

func NewResponseFormatter(prefix string, renderer *template.Renderer, opts ...Option) *ResponseFormatter {
	if stringutil.TrimSpace(prefix) == "" {
		prefix = "!"
	}

	f := &ResponseFormatter{prefix: prefix, renderer: renderer}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

func (f *ResponseFormatter) Prefix() string {
	if f == nil {
		return "!"
	}

	if trimmed := stringutil.TrimSpace(f.prefix); trimmed != "" {
		return trimmed
	}

	return "!"
}

// ResolveError는 message_strings error namespace의 문구를 돌려준다. 인자 key는 messaging.Err* 상수이며 bot plane
// 기동 검증이 모든 상수의 값을 보장한다. 코드 대체 문구는 두지 않는다
// (DEC-20260926-hololive-message-strings-startup-validation).
func (f *ResponseFormatter) ResolveError(_ context.Context, key string) string {
	if f == nil {
		return ""
	}

	return f.messageStrings.Text(messagestrings.Key{Namespace: messagestrings.NamespaceError, Name: key})
}

// renderFailureText는 템플릿을 렌더하지 못했을 때 사용자에게 보내는 문구다. 코드에 둔 대체 문구 대신
// message_strings의 command_processing_failed(기동 검증 대상)를 쓴다.
func (f *ResponseFormatter) renderFailureText() string {
	if f == nil {
		return ""
	}

	return f.messageStrings.Text(RenderFailureMessageKey)
}

func (f *ResponseFormatter) GraduatedMemberWarning(_ context.Context) string {
	if f == nil {
		return ""
	}

	return f.messageStrings.Text(messagestrings.NotifyGraduatedMemberWarning)
}

type memberNotFoundTemplateData struct {
	MemberName string
}

func (f *ResponseFormatter) MemberNotFound(ctx context.Context, memberName string) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberNotFound, memberNotFoundTemplateData{MemberName: memberName})
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}
