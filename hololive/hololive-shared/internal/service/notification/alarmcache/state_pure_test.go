package alarmcache

import (
	"testing"

	"github.com/stretchr/testify/require"

	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
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

	first := sharedalarmkeys.BuildTitleFingerprint("title", "vid1")
	repeated := sharedalarmkeys.BuildTitleFingerprint("title", "vid1")

	require.Equal(t, first, repeated)
	require.NotEqual(t, sharedalarmkeys.BuildTitleFingerprint("title-a", "vid1"), sharedalarmkeys.BuildTitleFingerprint("title-b", "vid1"))
}
