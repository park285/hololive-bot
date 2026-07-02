package imagehost

import "testing"

func TestAvatarHostsIsSubsetOfThumbnailHosts(t *testing.T) {
	t.Parallel()

	for _, host := range AvatarHosts.Hosts() {
		if !ThumbnailHosts.AllowsHost(host) {
			t.Errorf("avatar host %q is not covered by ThumbnailHosts — subset invariant broken", host)
		}
	}
}

func TestAvatarHostsRejectsThumbnailOnlyHosts(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"i.ytimg.com", "lh3.googleusercontent.com", "i9.ytimg.com"} {
		if AvatarHosts.AllowsHost(host) {
			t.Errorf("AvatarHosts must not allow thumbnail-only host %q", host)
		}
	}
}

func TestValidateURL(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"https://yt3.ggpht.com/avatar=s88-c",
		"https://yt3.googleusercontent.com/img=s288",
	}
	for _, raw := range allowed {
		if err := AvatarHosts.ValidateURL(raw); err != nil {
			t.Errorf("AvatarHosts.ValidateURL(%q) = %v, want nil", raw, err)
		}
	}

	rejected := []string{
		"http://yt3.ggpht.com/x",
		"https://yt3.ggpht.com:8080/x",
		"https://evil.com@yt3.ggpht.com/x",
		"https://127.0.0.1/x.jpg",
		"https://[::1]/x.jpg",
		"https://i.ytimg.com/vi/a/hq.jpg",
		"https://notytimg.com/x.jpg",
		"file:///etc/passwd",
		"https:///x.jpg",
	}
	for _, raw := range rejected {
		if err := AvatarHosts.ValidateURL(raw); err == nil {
			t.Errorf("AvatarHosts.ValidateURL(%q) = nil, want error", raw)
		}
	}

	if !ThumbnailHosts.AllowsURL("https://i9.ytimg.com/vi/abc/hqdefault.jpg") {
		t.Error("ThumbnailHosts must allow numbered ytimg subdomain via suffix")
	}
	if ThumbnailHosts.AllowsURL("https://notytimg.com/x.jpg") {
		t.Error("ThumbnailHosts must reject suffix lookalike host")
	}
}
