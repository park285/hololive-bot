package template

import "testing"

func TestTrustedLink(t *testing.T) {
	t.Parallel()

	trusted := []string{
		"https://www.youtube.com/watch?v=abc123",
		"https://youtube.com/watch?v=abc",
		"https://m.youtube.com/watch?v=abc",
		"https://www.youtube.com/live/abc",
		"https://youtube.com/live/abc",
		"https://youtu.be/abc",
		"https://short.holoshi.com/l/abc",
		"https://www.twitch.tv/name",
		"https://twitch.tv/name",
		"https://chzzk.naver.com/live/abc",
	}

	for _, url := range trusted {
		if got := trustedLink(url); got != url {
			t.Errorf("trusted URL %q: got %q", url, got)
		}
	}

	rejected := []string{
		"",
		"https://youtu.be/abc | https://chzzk.naver.com/live/xyz",
		"https://short.holoshi.com/l/abc | https://chzzk.naver.com/live/xyz",
		"https://youtu.be/a|b | https://chzzk.naver.com/live/xyz",
		"https://www.twitch.tv/name | https://chzzk.naver.com/live/xyz",
		"https://youtu.be/abc | https://youtu.be/def",
		"https://youtu.be/abc|https://chzzk.naver.com/live/xyz",
		"https://youtu.be/abc\nhttps://evil.example",
		"https://youtu.be/abc\r",
		"https://youtu.be/(abc)",
		"https://youtu.be/[abc]",
		"https://youtu.be/<abc>",
		"https://youtu.be/a\\b",
		"https://youtu.be/a\tb",
		"https://youtu.be/a b",
		"http://youtu.be/abc",
		"https://example.com/watch?v=abc",
		"https://youtube.com.evil.example/watch?v=abc",
		"**굵게** _기울임_ [링크](https://example.com)",
		"한글 URL 아님",
	}

	for _, url := range rejected {
		if got := trustedLink(url); got != "" {
			t.Errorf("untrusted URL %q: got %q, want empty", url, got)
		}
	}
}
