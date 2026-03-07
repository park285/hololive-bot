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

package providers

import (
	"testing"
)

func TestDefaultAlarmAdvanceMinute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []int
		want    int
	}{
		{
			name:  "빈 슬라이스 - 기본값 5 반환",
			input: []int{},
			want:  5,
		},
		{
			name:  "0만 있는 슬라이스 - 기본값 5 반환",
			input: []int{0, 0},
			want:  5,
		},
		{
			name:  "여러 값 - 최댓값 반환",
			input: []int{3, 7, 2},
			want:  7,
		},
		{
			name:  "단일 값 - 해당 값 반환",
			input: []int{1},
			want:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := defaultAlarmAdvanceMinute(tt.input)
			if got != tt.want {
				t.Errorf("defaultAlarmAdvanceMinute(%v) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
