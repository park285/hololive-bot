package alarmcache

import (
	"testing"

	"github.com/stretchr/testify/require"

	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
)

func TestFirstMemberName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidates []string
		want       string
	}{
		{name: "first non-blank wins", candidates: []string{"  ", "  short ", "ko"}, want: "short"},
		{name: "skips blanks", candidates: []string{"", "\t", "name"}, want: "name"},
		{name: "all blank", candidates: []string{"", "   "}, want: ""},
		{name: "no candidates", candidates: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, FirstMemberName(tt.candidates...))
		})
	}
}

func TestBuildTitleFingerprint(t *testing.T) {
	t.Parallel()

	first := alarmkeys.BuildTitleFingerprint("title", "vid1")
	repeated := alarmkeys.BuildTitleFingerprint("title", "vid1")

	require.Equal(t, first, repeated)
	require.NotEqual(t, alarmkeys.BuildTitleFingerprint("title-a", "vid1"), alarmkeys.BuildTitleFingerprint("title-b", "vid1"))
}
