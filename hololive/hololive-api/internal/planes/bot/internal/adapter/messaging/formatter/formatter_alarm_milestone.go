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
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func (f *ResponseFormatter) formatAlarmTypesLabel(ctx context.Context, types domain.AlarmTypes) string {
	if len(types) == 0 || len(types) == len(domain.AllAlarmTypes) {
		return f.messageStrings.GetContext(ctx, messagestrings.NamespaceAlarmType, "ALL")
	}

	names := make([]string, len(types))
	for i, t := range types {
		names[i] = f.messageStrings.GetContext(ctx, messagestrings.NamespaceAlarmType, t.String())
	}

	return strings.Join(names, "+")
}

type ambiguousMemberCandidate struct {
	Index int
	Name  string
}

type ambiguousMembersTemplateData struct {
	Prefix         string
	CommandExample string
	FirstName      string
	Candidates     []ambiguousMemberCandidate
}

func (f *ResponseFormatter) FormatAmbiguousMembers(ctx context.Context, candidates []*domain.Member, commandExample string) string {
	shaped := make([]ambiguousMemberCandidate, len(candidates))
	for i, m := range candidates {
		shaped[i] = ambiguousMemberCandidate{Index: i + 1, Name: m.GetDisplayName()}
	}

	firstName := ""
	if len(candidates) > 0 {
		firstName = candidates[0].GetDisplayName()
	}

	data := ambiguousMembersTemplateData{
		Prefix:         f.Prefix(),
		CommandExample: commandExample,
		FirstName:      firstName,
		Candidates:     shaped,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAmbiguousMember, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}
