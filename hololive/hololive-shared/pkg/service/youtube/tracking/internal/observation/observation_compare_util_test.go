package observation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestObservationComparisonTitleHintKey_NormalizesAndLowercases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "normal text", in: "Hello World", want: "hello world"},
		{name: "extra whitespace", in: "  Hello   World  ", want: "hello world"},
		{name: "empty", in: "", want: ""},
		{name: "whitespace only", in: "   ", want: ""},
		{name: "mixed case unicode", in: "  テスト   Title  ", want: "テスト title"},
		{name: "newlines collapsed", in: "line1\nline2\tline3", want: "line1 line2 line3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, observationComparisonTitleHintKey(tt.in))
		})
	}
}

func TestObservationComparisonTitleHintKey_TruncatesLongInput(t *testing.T) {
	t.Parallel()

	long := make([]rune, 200)
	for i := range long {
		long[i] = 'A'
	}
	result := observationComparisonTitleHintKey(string(long))
	require.LessOrEqual(t, len([]rune(result)), 120)
}

func TestTimeValueForObservationPostComparison_NilReturnsZero(t *testing.T) {
	t.Parallel()

	result := timeValueForObservationPostComparison(nil)
	require.True(t, result.IsZero())
}

func TestTimeValueForObservationPostComparison_NonNilReturnsUTC(t *testing.T) {
	t.Parallel()

	loc := time.FixedZone("KST", 9*60*60)
	input := time.Date(2026, 5, 10, 15, 30, 0, 0, loc)
	result := timeValueForObservationPostComparison(&input)
	require.Equal(t, time.UTC, result.Location())
	require.Equal(t, input.UTC(), result)
}
