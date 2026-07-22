package domain

import "testing"

func TestYouTubeOutboxIdentityAuthorityGolden(t *testing.T) {
	got := mustYouTubeIdentity(t, youtubeIdentityPayload("a", "b"))
	want := "sha256:b4c5b0a632b83c567feadef034033a5f4c1218e69440eff39142f0a6c0780e0d"
	if got != want {
		t.Fatalf("CanonicalIdentity() = %q, want %q", got, want)
	}
}
