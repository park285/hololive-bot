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
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestFormatMemberNewsSubscriptionMessages(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdMemberNewsNoMembers:    "NO_MEMBERS {{.Prefix}}",
		domain.TemplateKeyCmdMemberNewsSubscribed:   "SUBSCRIBED {{.Prefix}}",
		domain.TemplateKeyCmdMemberNewsAlreadySub:   "ALREADY_SUB {{.Prefix}}",
		domain.TemplateKeyCmdMemberNewsUnsubscribed: "UNSUBSCRIBED {{.Prefix}}",
		domain.TemplateKeyCmdMemberNewsNotSub:       "NOT_SUB {{.Prefix}}",
		domain.TemplateKeyCmdMemberNewsStatus:       "STATUS {{if .IsSubscribed}}ON{{else}}OFF{{end}} {{.Prefix}}",
	})
	formatter := NewResponseFormatter("?", renderer)

	ctx := t.Context()
	assert.Equal(t, "NO_MEMBERS ?", formatter.FormatMemberNewsNoMembers(ctx))
	assert.Equal(t, "SUBSCRIBED ?", formatter.FormatMemberNewsSubscribed(ctx))
	assert.Equal(t, "ALREADY_SUB ?", formatter.FormatMemberNewsAlreadySubscribed(ctx))
	assert.Equal(t, "UNSUBSCRIBED ?", formatter.FormatMemberNewsUnsubscribed(ctx))
	assert.Equal(t, "NOT_SUB ?", formatter.FormatMemberNewsNotSubscribed(ctx))
	assert.Equal(t, "STATUS ON ?", formatter.FormatMemberNewsStatus(ctx, true))
	assert.Equal(t, "STATUS OFF ?", formatter.FormatMemberNewsStatus(ctx, false))
}

func TestFormatMemberNewsSubscriptionMessages_RenderFailure(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	var logs bytes.Buffer

	formatter := NewResponseFormatter("!", setupFormatterTestRenderer(t, map[domain.TemplateKey]string{}),
		WithMessageStrings(setupFormatterTestStore(t)), WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))

	assert.Equal(t, renderFailureMessage, formatter.FormatMemberNewsNoMembers(ctx))
	assert.Equal(t, renderFailureMessage, formatter.FormatMemberNewsSubscribed(ctx))
	assert.Equal(t, renderFailureMessage, formatter.FormatMemberNewsAlreadySubscribed(ctx))
	assert.Equal(t, renderFailureMessage, formatter.FormatMemberNewsUnsubscribed(ctx))
	assert.Equal(t, renderFailureMessage, formatter.FormatMemberNewsNotSubscribed(ctx))
	assert.Equal(t, renderFailureMessage, formatter.FormatMemberNewsStatus(ctx, true))
	assert.Equal(t, renderFailureMessage, formatter.FormatMemberNewsStatus(ctx, false))

	wantKeys := []domain.TemplateKey{
		domain.TemplateKeyCmdMemberNewsNoMembers,
		domain.TemplateKeyCmdMemberNewsSubscribed,
		domain.TemplateKeyCmdMemberNewsAlreadySub,
		domain.TemplateKeyCmdMemberNewsUnsubscribed,
		domain.TemplateKeyCmdMemberNewsNotSub,
		domain.TemplateKeyCmdMemberNewsStatus,
		domain.TemplateKeyCmdMemberNewsStatus,
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	require.Len(t, lines, len(wantKeys))

	for i, key := range wantKeys {
		assert.Contains(t, lines[i], `level=ERROR msg="template render failed" template_key=`+string(key)+" error=")
	}
}
