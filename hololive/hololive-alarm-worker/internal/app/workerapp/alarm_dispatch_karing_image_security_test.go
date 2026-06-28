package workerapp

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestNormalizeKaringImageURLAllowsYouTubeImageHosts(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"video thumbnail", "https://i.ytimg.com/vi/abc/maxresdefault.jpg", "https://i.ytimg.com/vi/abc/maxresdefault.jpg"},
		{"author photo ggpht", "https://yt3.ggpht.com/avatar=s800", "https://yt3.ggpht.com/avatar=s800"},
		{"community googleusercontent", "https://yt3.googleusercontent.com/img=s288", "https://yt3.googleusercontent.com/img=s288"},
		{"lh3 googleusercontent", "https://lh3.googleusercontent.com/img", "https://lh3.googleusercontent.com/img"},
		{"numbered ytimg subdomain", "https://i9.ytimg.com/vi/abc/hqdefault.jpg", "https://i9.ytimg.com/vi/abc/hqdefault.jpg"},
		{"protocol relative gets https", "//yt3.ggpht.com/avatar=s88", "https://yt3.ggpht.com/avatar=s88"},
		{"trailing whitespace trimmed", "  https://i.ytimg.com/vi/abc/mqdefault.jpg  ", "https://i.ytimg.com/vi/abc/mqdefault.jpg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeKaringImageURL(tc.raw); got != tc.want {
				t.Fatalf("normalizeKaringImageURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestBuildAlarmDispatchNotificationKaringContentItemSanitizesThumbnail(t *testing.T) {
	safe := "https://i.ytimg.com/vi/abc/maxresdefault.jpg"
	photo := "https://yt3.ggpht.com/avatar=s800"
	imds := "https://169.254.169.254/latest/meta-data/"

	cases := []struct {
		name         string
		streamThumb  *string
		channelPhoto *string
		want         string
	}{
		{"safe stream thumbnail passes", &safe, nil, safe},
		{"unsafe stream thumbnail dropped", &imds, nil, ""},
		{"unsafe stream thumbnail falls back to safe channel photo", &imds, &photo, photo},
		{"unsafe channel photo dropped", nil, &imds, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			notification := &domain.AlarmNotification{
				Stream:  &domain.Stream{ID: "stream-1", Thumbnail: tc.streamThumb},
				Channel: &domain.Channel{Name: "Member", Photo: tc.channelPhoto},
			}
			if got := buildAlarmDispatchNotificationKaringContentItem(t.Context(), nil, notification).ThumbnailURL; got != tc.want {
				t.Fatalf("ThumbnailURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeKaringImageURLDropsUnsafeURLs(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"http scheme", "http://i.ytimg.com/vi/abc/maxresdefault.jpg"},
		{"private ip", "https://10.0.0.1/x.jpg"},
		{"loopback ip", "https://127.0.0.1/x.jpg"},
		{"link local metadata", "https://169.254.169.254/latest/meta-data/"},
		{"ipv6 loopback", "https://[::1]/x.jpg"},
		{"decimal ip literal", "https://2130706433/x.jpg"},
		{"disallowed host", "https://evil.example.com/x.jpg"},
		{"suffix lookalike host", "https://notytimg.com/x.jpg"},
		{"non standard port", "https://i.ytimg.com:8080/x.jpg"},
		{"embedded userinfo", "https://evil.com@i.ytimg.com/x.jpg"},
		{"file scheme", "file:///etc/passwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeKaringImageURL(tc.raw); got != "" {
				t.Fatalf("normalizeKaringImageURL(%q) = %q, want \"\" (dropped)", tc.raw, got)
			}
		})
	}
}
