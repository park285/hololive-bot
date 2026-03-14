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

package bot

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestShouldExecuteAsync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmdType domain.CommandType
		want    bool
	}{
		{name: "help", cmdType: domain.CommandHelp, want: true},
		{name: "live", cmdType: domain.CommandLive, want: true},
		{name: "alarm add", cmdType: domain.CommandAlarmAdd, want: false},
		{name: "alarm list", cmdType: domain.CommandAlarmList, want: false},
		{name: "member news digest", cmdType: domain.CommandMemberNews, want: false},
		{name: "news subscription", cmdType: domain.CommandMemberNewsSubscription, want: false},
		{name: "major event", cmdType: domain.CommandMajorEvent, want: false},
		{name: "custom command", cmdType: domain.CommandType("custom"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldExecuteAsync(tt.cmdType); got != tt.want {
				t.Fatalf("shouldExecuteAsync(%q) = %v, want %v", tt.cmdType, got, tt.want)
			}
		})
	}
}
