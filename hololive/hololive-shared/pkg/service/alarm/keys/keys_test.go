package keys

import "testing"

func TestBuildTitleFingerprint_FullWidthPunctuationEquivalence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		titleA string
		titleB string
	}{
		{
			name:   "half-width vs full-width exclamation",
			titleA: "クリアする!そして",
			titleB: "クリアする！そして",
		},
		{
			name:   "half-width vs full-width question",
			titleA: "本当に?マジで",
			titleB: "本当に？マジで",
		},
		{
			name:   "mixed full-width punctuation",
			titleA: "テスト!方送(開始)",
			titleB: "テスト！方送（開始）",
		},
		{
			name:   "brackets lenticular",
			titleA: "【ホロライブ】配信",
			titleB: "ホロライブ配信",
		},
		}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fpA := BuildTitleFingerprint(tt.titleA, "stream-1")
			fpB := BuildTitleFingerprint(tt.titleB, "stream-1")
			if fpA != fpB {
				t.Errorf("fingerprints differ: %q != %q (titleA=%q, titleB=%q)", fpA, fpB, tt.titleA, tt.titleB)
			}
		})
	}
}

func TestBuildTitleFingerprint_DifferentTitles(t *testing.T) {
	t.Parallel()

	fpA := BuildTitleFingerprint("Minecraft配信", "s1")
	fpB := BuildTitleFingerprint("Pokemon配信", "s1")
	if fpA == fpB {
		t.Error("different titles should produce different fingerprints")
	}
}
