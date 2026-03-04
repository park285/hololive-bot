package util

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/valkey-io/valkey-go"
)

func TestMax(t *testing.T) {
	t.Parallel()

	if got := Max(10, 3); got != 10 {
		t.Fatalf("Max(10, 3) = %d, want 10", got)
	}
	if got := Max(-1, 5); got != 5 {
		t.Fatalf("Max(-1, 5) = %d, want 5", got)
	}
}

func TestMin(t *testing.T) {
	t.Parallel()

	if got := Min(10, 3); got != 3 {
		t.Fatalf("Min(10, 3) = %d, want 3", got)
	}
	if got := Min(-1, 5); got != -1 {
		t.Fatalf("Min(-1, 5) = %d, want -1", got)
	}
}

func TestUnique(t *testing.T) {
	t.Parallel()

	got := Unique([]int{3, 1, 3, 2, 1, 4, 2})
	want := []int{3, 1, 2, 4}
	if len(got) != len(want) {
		t.Fatalf("len(Unique()) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Unique()[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestFormatKoreanNumber(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   int64
		want string
	}{
		{name: "less than ten-thousand", in: 500, want: "500"},
		{name: "exact ten-thousand", in: 10000, want: "1만"},
		{name: "with remainder", in: 12345, want: "1만 2345"},
		{name: "large exact", in: 250000, want: "25만"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FormatKoreanNumber(tc.in); got != tc.want {
				t.Fatalf("FormatKoreanNumber(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeSuffix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "trim and remove 짱", in: "  후부짱 ", want: "후부"},
		{name: "remove 쨩", in: "미코쨩", want: "미코"},
		{name: "no suffix", in: "스이세이", want: "스이세이"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeSuffix(tc.in); got != tc.want {
				t.Fatalf("NormalizeSuffix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestApplyKakaoSeeMorePadding(t *testing.T) {
	t.Parallel()

	t.Run("blank text returns original text", func(t *testing.T) {
		t.Parallel()
		const text = "   "
		if got := ApplyKakaoSeeMorePadding(text, "안내"); got != text {
			t.Fatalf("ApplyKakaoSeeMorePadding() = %q, want original %q", got, text)
		}
	})

	t.Run("instruction and body with auto newline", func(t *testing.T) {
		t.Parallel()
		got := ApplyKakaoSeeMorePadding("본문", "안내")
		if !strings.HasPrefix(got, "안내") {
			t.Fatalf("message prefix = %q, want starts with 안내", got)
		}
		if strings.Count(got, KakaoZeroWidthSpace) != KakaoSeeMorePadding {
			t.Fatalf("zero-width-space count = %d, want %d", strings.Count(got, KakaoZeroWidthSpace), KakaoSeeMorePadding)
		}
		if !strings.Contains(got, "\n본문") {
			t.Fatalf("message does not contain expected body separator: %q", got)
		}
	})

	t.Run("body already starts with newline", func(t *testing.T) {
		t.Parallel()
		got := ApplyKakaoSeeMorePadding("\n본문", "안내")
		doubleNewlineMarker := strings.Repeat(KakaoZeroWidthSpace, KakaoSeeMorePadding) + "\n\n본문"
		if strings.Contains(got, doubleNewlineMarker) {
			t.Fatalf("unexpected double newline in %q", got)
		}
	})
}

func TestIsValkeyNil(t *testing.T) {
	t.Parallel()

	t.Run("nil error", func(t *testing.T) {
		t.Parallel()
		if IsValkeyNil(nil) {
			t.Fatal("IsValkeyNil(nil) = true, want false")
		}
	})

	t.Run("direct valkey nil", func(t *testing.T) {
		t.Parallel()
		if !IsValkeyNil(valkey.Nil) {
			t.Fatal("IsValkeyNil(valkey.Nil) = false, want true")
		}
	})

	t.Run("wrapped valkey nil", func(t *testing.T) {
		t.Parallel()
		wrapped := fmt.Errorf("outer: %w", valkey.Nil)
		doubleWrapped := fmt.Errorf("outer2: %w", wrapped)
		if !IsValkeyNil(doubleWrapped) {
			t.Fatal("IsValkeyNil(doubleWrapped valkey.Nil) = false, want true")
		}
	})

	t.Run("non-valkey error", func(t *testing.T) {
		t.Parallel()
		if IsValkeyNil(errors.New("other")) {
			t.Fatal("IsValkeyNil(non-valkey) = true, want false")
		}
	})
}
