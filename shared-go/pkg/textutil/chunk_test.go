package textutil

import (
	"strings"
	"testing"
)

func TestChunkByLines(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLength int
		want      int
	}{
		{
			name:      "empty string",
			input:     "",
			maxLength: 100,
			want:      0,
		},
		{
			name:      "single short line",
			input:     "hello",
			maxLength: 100,
			want:      1,
		},
		{
			name:      "multiple lines within limit",
			input:     "line1\nline2\nline3",
			maxLength: 100,
			want:      1,
		},
		{
			name:      "long line requiring split",
			input:     strings.Repeat("a", 150),
			maxLength: 100,
			want:      2,
		},
		{
			name:      "mixed short and long lines",
			input:     "short\n" + strings.Repeat("b", 150) + "\nshort2",
			maxLength: 100,
			want:      3,
		},
		{
			name:      "zero maxLength",
			input:     "hello\nworld",
			maxLength: 0,
			want:      1,
		},
		{
			name:      "negative maxLength",
			input:     "hello\nworld",
			maxLength: -1,
			want:      1,
		},
		{
			name:      "korean characters",
			input:     "안녕하세요\n반갑습니다",
			maxLength: 20,
			want:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ChunkByLines(tt.input, tt.maxLength)
			if len(got) != tt.want {
				t.Errorf("ChunkByLines() returned %d chunks, want %d", len(got), tt.want)
			}

			for i, chunk := range got {
				if len([]rune(chunk)) > tt.maxLength && tt.maxLength > 0 {
					t.Errorf("chunk[%d] length %d exceeds maxLength %d", i, len([]rune(chunk)), tt.maxLength)
				}
			}
		})
	}
}

func TestKakaoMessageMaxLength(t *testing.T) {
	if KakaoMessageMaxLength != 500 {
		t.Errorf("KakaoMessageMaxLength = %d, want 500", KakaoMessageMaxLength)
	}
}
