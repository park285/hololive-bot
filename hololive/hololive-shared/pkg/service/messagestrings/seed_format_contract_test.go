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

package messagestrings_test

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func TestSeedFormatStrings_VerbContract(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}

	formatCases := []struct {
		namespace string
		key       string
		args      []any
	}{
		{messagestrings.NamespaceTimeFmt, "stream_time_days", []any{"01/02 15:04", 3}},
		{messagestrings.NamespaceTimeFmt, "stream_time_hours_minutes", []any{"01/02 15:04", 2, 30}},
		{messagestrings.NamespaceTimeFmt, "stream_time_minutes", []any{"01/02 15:04", 45}},
		{messagestrings.NamespaceTimeFmt, "relative_days", []any{3}},
		{messagestrings.NamespaceTimeFmt, "relative_hours_minutes", []any{2, 30}},
		{messagestrings.NamespaceTimeFmt, "relative_minutes", []any{45}},
		{messagestrings.NamespaceKaring, "alarm_title_prelive", []any{5}},
		{messagestrings.NamespaceKaring, "time_left_prelive", []any{5}},
		{messagestrings.NamespaceKaring, "count_suffix", []any{"방송 알림", 3}},
	}
	for _, c := range formatCases {
		value := store.Get(c.namespace, c.key)
		if value == "" {
			t.Errorf("Get(%q, %q) is empty; seed missing", c.namespace, c.key)
			continue
		}
		rendered := fmt.Sprintf(value, c.args...)
		if strings.Contains(rendered, "%!") {
			t.Errorf("Sprintf(%s/%s) verb mismatch: %q", c.namespace, c.key, rendered)
		}
	}

	staticKaringKeys := []string{
		"alarm_title_live", "time_left_live",
		"outbox_title_community", "outbox_time_community",
		"outbox_title_shorts", "outbox_time_shorts",
		"outbox_title_video", "outbox_time_video",
		"outbox_title_live", "outbox_time_live",
		"title_fallback", "time_fallback",
		"item_title_community_fallback",
		"status_community", "status_shorts", "status_video", "status_fallback",
	}
	for _, key := range staticKaringKeys {
		value := store.Get(messagestrings.NamespaceKaring, key)
		if value == "" {
			t.Errorf("Get(karing, %q) is empty; seed missing", key)
			continue
		}
		if strings.Contains(value, "%") {
			t.Errorf("static karing key %q unexpectedly contains format verb: %q", key, value)
		}
	}
}
