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

package dbx

import (
	"encoding/base64"
	"testing"
)

func TestEncodeCursor(t *testing.T) {
	tests := []struct {
		name      string
		field     string
		value     any
		direction string
		wantErr   bool
	}{
		{"valid DESC", "achieved_at", "2026-01-20T10:00:00Z", "DESC", false},
		{"valid ASC", "id", 123, "ASC", false},
		{"empty field", "", "value", "DESC", true},
		{"invalid field injection", "id;DROP TABLE users", "value", "DESC", true},
		{"default direction", "field", "value", "INVALID", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := EncodeCursor(tt.field, tt.value, tt.direction)
			if (err != nil) != tt.wantErr {
				t.Errorf("EncodeCursor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && token == "" {
				t.Error("EncodeCursor() returned empty token")
			}
		})
	}
}

func TestDecodeCursor(t *testing.T) {
	maliciousFieldToken := base64.URLEncoding.EncodeToString([]byte(`{"f":"id;DROP TABLE users","v":1,"d":"DESC"}`))
	oversizedToken := make([]byte, maxCursorTokenLength+1)
	for i := range oversizedToken {
		oversizedToken[i] = 'a'
	}

	tests := []struct {
		name      string
		token     string
		wantField string
		wantErr   bool
	}{
		{"empty token", "", "", false},
		{"invalid base64", "!!!invalid!!!", "", true},
		{"invalid json", "aW52YWxpZA==", "", true},
		{"invalid field", maliciousFieldToken, "", true},
		{"oversized token", string(oversizedToken), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := DecodeCursor(tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeCursor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.token != "" && data == nil {
				t.Error("DecodeCursor() returned nil for valid token")
			}
		})
	}
}

func TestEncodeDecode(t *testing.T) {
	field := "achieved_at"
	value := "2026-01-20T10:00:00Z"
	direction := "DESC"

	token, err := EncodeCursor(field, value, direction)
	if err != nil {
		t.Fatalf("EncodeCursor failed: %v", err)
	}

	data, err := DecodeCursor(token)
	if err != nil {
		t.Fatalf("DecodeCursor failed: %v", err)
	}

	if data.Field != field {
		t.Errorf("Field = %q, want %q", data.Field, field)
	}
	if data.Direction != direction {
		t.Errorf("Direction = %q, want %q", data.Direction, direction)
	}
}

func TestBuildKeysetCondition(t *testing.T) {
	tests := []struct {
		name          string
		cursor        *CursorData
		paramIndex    int
		wantClause    string
		wantArgsCount int
	}{
		{"nil cursor", nil, 1, "", 0},
		{
			"DESC cursor",
			&CursorData{Field: "achieved_at", Value: "2026-01-20", Direction: "DESC"},
			1,
			"achieved_at < $1",
			1,
		},
		{
			"ASC cursor",
			&CursorData{Field: "id", Value: 100, Direction: "ASC"},
			2,
			"id > $2",
			1,
		},
		{
			"invalid field",
			&CursorData{Field: "id;DROP TABLE users", Value: 100, Direction: "ASC"},
			1,
			"",
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clause, args := BuildKeysetCondition(tt.cursor, tt.paramIndex)
			if clause != tt.wantClause {
				t.Errorf("clause = %q, want %q", clause, tt.wantClause)
			}
			if len(args) != tt.wantArgsCount {
				t.Errorf("args count = %d, want %d", len(args), tt.wantArgsCount)
			}
		})
	}
}
