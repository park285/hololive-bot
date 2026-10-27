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

package activity

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"testing"
)

func TestActivityLoggerWritesStructuredRecord(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	l := NewActivityLogger(slog.New(slog.NewJSONHandler(&buf, nil)))
	l.Log("settings_update", "Settings updated", map[string]any{"alarm_advance_minutes": 5})

	var record struct {
		Msg     string         `json:"msg"`
		Type    string         `json:"type"`
		Summary string         `json:"summary"`
		Details map[string]any `json:"details"`
	}

	if err := jsonv2.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("decode activity record: %v (%s)", err, buf.String())
	}

	if record.Msg != "activity" || record.Type != "settings_update" || record.Summary != "Settings updated" || record.Details["alarm_advance_minutes"] != float64(5) {
		t.Fatalf("activity record = %+v", record)
	}
}
