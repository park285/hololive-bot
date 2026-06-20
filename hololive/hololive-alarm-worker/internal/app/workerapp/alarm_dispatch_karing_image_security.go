package workerapp

import (
	"net/netip"
	"net/url"
	"strings"
)

var karingImageAllowedHosts = map[string]struct{}{
	"i.ytimg.com":               {},
	"img.youtube.com":           {},
	"yt3.ggpht.com":             {},
	"yt4.ggpht.com":             {},
	"yt3.googleusercontent.com": {},
	"yt4.googleusercontent.com": {},
	"lh3.googleusercontent.com": {},
	"lh4.googleusercontent.com": {},
	"lh5.googleusercontent.com": {},
	"lh6.googleusercontent.com": {},
}

var karingImageAllowedHostSuffixes = []string{
	".ytimg.com",
	".ggpht.com",
}

func isAllowedKaringImageURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" || parsed.User != nil {
		return false
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	return isAllowedKaringImageHost(host)
}

func isAllowedKaringImageHost(host string) bool {
	if _, ok := karingImageAllowedHosts[host]; ok {
		return true
	}
	for _, suffix := range karingImageAllowedHostSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}
