package imagehost

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

type Policy struct {
	exact    map[string]struct{}
	suffixes []string
}

func NewPolicy(hosts []string, suffixes []string) Policy {
	exact := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		exact[NormalizeHost(h)] = struct{}{}
	}
	return Policy{exact: exact, suffixes: suffixes}
}

// AvatarHosts는 봇 아바타 경로(멤버 채널 사진) 전용의 좁은 셋이다.
// ThumbnailHosts의 서브셋을 유지해야 하며(테스트로 핀), 아바타 경로에
// 썸네일 호스트·와일드카드를 부여해 SSRF 표면을 넓히지 않는다.
var AvatarHosts = NewPolicy([]string{
	"yt3.ggpht.com",
	"yt3.googleusercontent.com",
}, nil)

var ThumbnailHosts = NewPolicy([]string{
	"i.ytimg.com",
	"img.youtube.com",
	"yt3.ggpht.com",
	"yt4.ggpht.com",
	"yt3.googleusercontent.com",
	"yt4.googleusercontent.com",
	"lh3.googleusercontent.com",
	"lh4.googleusercontent.com",
	"lh5.googleusercontent.com",
	"lh6.googleusercontent.com",
}, []string{
	".ytimg.com",
	".ggpht.com",
})

func (p Policy) ValidateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse image url: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("image url scheme %q is not allowed", parsed.Scheme)
	}
	if parsed.User != nil {
		return errors.New("image url must not carry userinfo")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return fmt.Errorf("image url port %q is not allowed", port)
	}
	host := NormalizeHost(parsed.Hostname())
	if host == "" {
		return errors.New("image url host is empty")
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return fmt.Errorf("image url host %q must not be an IP literal", host)
	}
	if !p.AllowsHost(host) {
		return fmt.Errorf("image host %q is not allowed", host)
	}
	return nil
}

func (p Policy) AllowsURL(raw string) bool {
	return p.ValidateURL(raw) == nil
}

func (p Policy) AllowsHost(host string) bool {
	host = NormalizeHost(host)
	if _, ok := p.exact[host]; ok {
		return true
	}
	for _, suffix := range p.suffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func (p Policy) Hosts() []string {
	hosts := make([]string, 0, len(p.exact))
	for h := range p.exact {
		hosts = append(hosts, h)
	}
	return hosts
}

func NormalizeHost(hostname string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
}
