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
