package settings

import (
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	shortlinkcontracts "github.com/kapu/hololive-shared/pkg/contracts/shortlink"
)

var nginxLocationPattern = regexp.MustCompile(`(?m)^\s*location\s+`)

const centralIngressBindPlaceholder = "@BIND_IP@"

func repoRootFromConfigTest(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", ".."))
}

func readRepoFile(t *testing.T, relativePath string) string {
	t.Helper()

	content, err := fs.ReadFile(os.DirFS(repoRootFromConfigTest(t)), repoLocalPath(t, relativePath))
	if err != nil {
		t.Fatalf("read %s failed: %v", relativePath, err)
	}

	return string(content)
}

func repoLocalPath(t *testing.T, relativePath string) string {
	t.Helper()

	if filepath.IsAbs(relativePath) {
		t.Fatalf("repo path %q must be relative", relativePath)
	}

	clean := filepath.Clean(relativePath)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		t.Fatalf("repo path %q escapes repo root", relativePath)
	}

	return filepath.ToSlash(clean)
}

func TestRepoPublicIngressRoutesMatchCentralListeners(t *testing.T) {
	publicTemplate := readRepoFile(t, "deploy/nginx/holoshi-public-shortlink.conf")
	if count := len(nginxLocationPattern.FindAllString(publicTemplate, -1)); count != 3 {
		t.Fatalf("public ingress location count = %d, want 3", count)
	}

	upstreamBlock, publicPort := publicShortLinkUpstream(t, publicTemplate)
	centralPort := centralShortLinkPort(t)

	if publicPort != centralPort {
		t.Fatalf("public upstream port %q != central short-link listen port %q", publicPort, centralPort)
	}

	assertExactNginxDirectives(t, upstreamBlock, "keepalive", []string{"8"})
	assertPublicShortLinkServer(t, publicTemplate)
}

func publicShortLinkUpstream(t *testing.T, publicTemplate string) (string, string) {
	t.Helper()

	upstreamBlock := nginxBlockContaining(t, publicTemplate, "upstream shortlink_backend {", "server")
	upstreamServers := nginxDirectiveValues(upstreamBlock, "server")

	if len(upstreamServers) != 1 {
		t.Fatalf("public upstream server directives = %q, want exactly one", upstreamServers)
	}

	publicHost, publicPort, found := strings.Cut(upstreamServers[0], ":")
	if !found || publicHost == "" || publicPort == "" {
		t.Fatalf("public upstream server %q is not host:port", upstreamServers[0])
	}

	if net.ParseIP(publicHost) == nil {
		t.Fatalf("public upstream host %q is not a literal IP; the public ingress is applied verbatim on the gateway", publicHost)
	}

	return upstreamBlock, publicPort
}

func centralShortLinkPort(t *testing.T) string {
	t.Helper()

	centralConfig := readRepoFile(t, "deploy/nginx/admin-dashboard-ingress.conf.template")
	centralShortLink := nginxBlockContaining(t, centralConfig, "server {", "location ^~ "+shortlinkcontracts.YouTubePathPrefix)
	shortLinkListen := nginxDirectiveValues(centralShortLink, "listen")

	if len(shortLinkListen) != 1 {
		t.Fatalf("central short-link listen directives = %q, want exactly one", shortLinkListen)
	}

	centralHost, centralPort, ok := strings.Cut(shortLinkListen[0], ":")
	if !ok || centralHost != centralIngressBindPlaceholder {
		t.Fatalf("central short-link listen = %q, want %s:<port>", shortLinkListen[0], centralIngressBindPlaceholder)
	}

	return centralPort
}

func assertPublicShortLinkServer(t *testing.T, publicTemplate string) {
	t.Helper()

	publicServer := nginxBlockContaining(t, publicTemplate, "server {", "server_name short.holoshi.com;")
	assertExactNginxDirectives(t, publicServer, "server_name", []string{"short.holoshi.com"})
	assertExactNginxDirectives(t, publicServer, "listen", []string{"443 quic", "443 ssl"})

	publicShortLink := nginxBlockContaining(t, publicServer, "location ^~ "+shortlinkcontracts.YouTubePathPrefix, "proxy_pass")
	assertExactNginxDirectives(t, publicShortLink, "proxy_pass", []string{"http://shortlink_backend"})
	assertExactNginxDirectives(t, publicShortLink, "limit_req", []string{"zone=holoshi_shortlink_requests burst=40 nodelay"})
	assertExactNginxDirectives(t, publicShortLink, "limit_conn", []string{"holoshi_shortlink_connections 16"})

	kakaoDeepLink := nginxBlockContaining(t, publicServer, "location ^~ /k/", "return 302 $kakao_deep_link_target;")
	assertExactNginxDirectives(t, kakaoDeepLink, "limit_req", []string{"zone=holoshi_shortlink_requests burst=40 nodelay"})
	assertExactNginxDirectives(t, kakaoDeepLink, "limit_conn", []string{"holoshi_shortlink_connections 16"})

	assertPublicKakaoDeepLinkDirectives(t, publicTemplate)

	catchAll := nginxBlockContaining(t, publicServer, "location /", "return 404;")
	assertExactNginxDirectives(t, catchAll, "return", []string{"404"})
}

func assertPublicKakaoDeepLinkDirectives(t *testing.T, publicTemplate string) {
	t.Helper()

	for _, required := range []string{
		"map $request_uri $kakao_deep_link_target {",
		"map $http_user_agent $kakao_deep_link_scraper {",
		"map $request_method $kakao_deep_link_get {",
		"map $uri $shortlink_access_log_enabled {",
		"access_log /dev/stdout ingress_json if=$shortlink_access_log_enabled;",
		"if ($kakao_deep_link_target = \"\") {",
		"if ($kakao_deep_link_scraper) {",
		"if ($kakao_deep_link_get = 0) {",
		"add_header_inherit on;",
		"return 404;",
		"return 403;",
		"add_header Cache-Control \"no-store, max-age=0\" always;",
		"add_header Referrer-Policy \"no-referrer\" always;",
		"add_header Vary \"User-Agent\" always;",
		"add_header X-Robots-Tag \"noindex, nofollow, noarchive, nosnippet\" always;",
	} {
		if !strings.Contains(publicTemplate, required) {
			t.Fatalf("public Kakao deep-link ingress missing %q", required)
		}
	}
}

func nginxBlockContaining(t *testing.T, content, anchor, required string) string {
	t.Helper()

	searchFrom := 0

	for {
		relative := strings.Index(content[searchFrom:], anchor)
		if relative < 0 {
			break
		}

		start := searchFrom + relative
		block := nginxBalancedBlock(t, content, start)

		if strings.Contains(block, required) {
			return block
		}

		searchFrom = start + len(anchor)
	}

	t.Fatalf("nginx %q block containing %q is missing", anchor, required)

	return ""
}

func nginxBalancedBlock(t *testing.T, content string, anchorIndex int) string {
	t.Helper()

	openRelative := strings.Index(content[anchorIndex:], "{")
	if openRelative < 0 {
		t.Fatalf("nginx block at offset %d has no opening brace", anchorIndex)
	}

	open := anchorIndex + openRelative
	depth := 0

	for index := open; index < len(content); index++ {
		switch content[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[anchorIndex : index+1]
			}
		}
	}

	t.Fatalf("nginx block at offset %d is unbalanced", anchorIndex)

	return ""
}

func assertExactNginxDirectives(t *testing.T, block, directive string, want []string) {
	t.Helper()

	got := nginxDirectiveValues(block, directive)

	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Fatalf("nginx %s directives = %q, want %q", directive, got, want)
	}
}

func nginxDirectiveValues(block, directive string) []string {
	pattern := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(directive) + `\s+([^;]+);\s*$`)
	matches := pattern.FindAllStringSubmatch(block, -1)
	got := make([]string, 0, len(matches))

	for _, match := range matches {
		got = append(got, strings.TrimSpace(match[1]))
	}

	slices.Sort(got)

	return got
}
