package providerhttp

import (
	"net/url"
	"path"
	"strings"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func ParseHolodexBaseURL(raw string) (*url.URL, error) {
	parsed, err := parseProviderBaseURL(raw, "holodex")
	if err != nil {
		return nil, err
	}
	if !validHolodexPath(parsed.Path) {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "holodex base URL path prefix is invalid")
	}
	return parsed, nil
}

func ParseOfficialScheduleBaseURL(raw string) (*url.URL, error) {
	parsed, err := parseProviderBaseURL(raw, "official schedule")
	if err != nil {
		return nil, err
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "official schedule base URL must be an HTTPS origin")
	}
	return parsed, nil
}

func parseProviderBaseURL(raw, name string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, name+" base URL is invalid")
	}
	if parsed.User != nil {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, name+" base URL must not include userinfo")
	}
	if !isHTTPSProviderURL(parsed) {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, name+" base URL must be HTTPS")
	}
	if hasDisallowedProviderURLParts(parsed) {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, name+" base URL must not include query, fragment, or escaped path")
	}
	copied := *parsed
	return &copied, nil
}

func isHTTPSProviderURL(parsed *url.URL) bool {
	return parsed.Scheme == "https" && parsed.Host != "" && parsed.Opaque == ""
}

func hasDisallowedProviderURLParts(parsed *url.URL) bool {
	return parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != ""
}

func validHolodexPath(value string) bool {
	if value == "" || value == "/" {
		return true
	}
	if !strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return false
	}
	return path.Clean(value) == value
}
