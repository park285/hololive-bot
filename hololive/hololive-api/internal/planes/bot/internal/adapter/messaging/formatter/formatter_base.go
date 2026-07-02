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

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/stringutil"
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

func (f *ResponseFormatter) foldSeeMore(s string) string {
	if f == nil || !f.seeMoreFold {
		return s
	}
	return util.FoldForSeeMore(s, util.KakaoSeeMoreThreshold)
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

func (f *ResponseFormatter) ResolveError(key string) string {
	return f.messageStrings.GetOr(messagestrings.NamespaceError, key, messagestrings.FallbackSentinel)
}

func (f *ResponseFormatter) GraduatedMemberWarning() string {
	return f.messageStrings.Get(messagestrings.NamespaceNotify, "graduated_member_warning")
}

type memberNotFoundTemplateData struct {
	MemberName string
}

func (f *ResponseFormatter) MemberNotFound(ctx context.Context, memberName string) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberNotFound, memberNotFoundTemplateData{MemberName: memberName})
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}
