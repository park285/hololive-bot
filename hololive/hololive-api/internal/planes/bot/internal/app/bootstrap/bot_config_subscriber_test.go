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

package bootstrap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersistedTargetMinutesResolvesConfiguredTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		alarmAdvanceMinutes int
		targetMinutes       []int
		want                []int
	}{
		{
			name:                "deduplicates and sorts configured targets",
			alarmAdvanceMinutes: 10,
			targetMinutes:       []int{5, 10, 5, 0, 1},
			want:                []int{10, 5, 1},
		},
		{
			name:                "single configured target expands through runtime policy",
			alarmAdvanceMinutes: 3,
			targetMinutes:       []int{10},
			want:                []int{10, 3, 1},
		},
		{
			name:                "explicit configured targets preserve configured policy",
			alarmAdvanceMinutes: 10,
			targetMinutes:       []int{10, 5, 1},
			want:                []int{10, 5, 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := PersistedTargetMinutes(tc.alarmAdvanceMinutes, tc.targetMinutes)

			assert.Equal(t, tc.want, got)
			require.NotEmpty(t, got)
		})
	}
}

func TestPersistedTargetMinutesFallsBackToRuntimeTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		alarmAdvanceMinutes int
		targetMinutes       []int
		want                []int
	}{
		{
			name:                "advance zero uses default targets",
			alarmAdvanceMinutes: 0,
			targetMinutes:       nil,
			want:                []int{5, 3, 1},
		},
		{
			name:                "advance one resolves to one minute",
			alarmAdvanceMinutes: 1,
			targetMinutes:       nil,
			want:                []int{1},
		},
		{
			name:                "advance two includes final minute",
			alarmAdvanceMinutes: 2,
			targetMinutes:       nil,
			want:                []int{2, 1},
		},
		{
			name:                "advance three includes final minute",
			alarmAdvanceMinutes: 3,
			targetMinutes:       []int{},
			want:                []int{3, 1},
		},
		{
			name:                "advance ten includes three and one",
			alarmAdvanceMinutes: 10,
			targetMinutes:       nil,
			want:                []int{10, 3, 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := PersistedTargetMinutes(tc.alarmAdvanceMinutes, tc.targetMinutes)

			assert.Equal(t, tc.want, got)
			require.NotEmpty(t, got)
		})
	}
}
