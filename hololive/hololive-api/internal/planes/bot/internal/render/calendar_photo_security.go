package render

import (
	"context"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/net/imagehost"
	"github.com/park285/shared-go/pkg/netguard"
)

const (
	calendarPhotoRequestTimeout = 5 * time.Second
	calendarPhotoMaxRedirects   = 2
)

var calendarPhotoAllowedContentTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

type calendarPhotoResolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

type calendarPhotoDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

var (
	calendarPhotoDNSResolver   calendarPhotoResolver = net.DefaultResolver
	calendarPhotoNetworkDialer calendarPhotoDialer   = &net.Dialer{Timeout: calendarPhotoRequestTimeout}
)

func newCalendarPhotoHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       calendarPhotoRequestTimeout,
		Transport:     newCalendarPhotoTransport(),
		CheckRedirect: checkCalendarPhotoRedirect,
	}
}

func newCalendarPhotoTransport() *http.Transport {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return netguard.GuardedTransport(&http.Transport{
			Proxy:       nil,
			DialContext: calendarPhotoNetworkDialer.DialContext,
		}, calendarPhotoNetguardPolicy())
	}
	transport := baseTransport.Clone()
	transport.Proxy = nil
	transport.DialContext = calendarPhotoNetworkDialer.DialContext
	return netguard.GuardedTransport(transport, calendarPhotoNetguardPolicy())
}

func validateCalendarPhotoURL(rawURL string) error {
	if err := imagehost.AvatarHosts.ValidateURL(rawURL); err != nil {
		return fmt.Errorf("calendar photo url: %w", err)
	}
	return nil
}

func checkCalendarPhotoRedirect(req *http.Request, via []*http.Request) error {
	return netguard.RedirectPolicy(netguard.RedirectConfig{
		Policy:       calendarPhotoNetguardPolicy(),
		MaxRedirects: calendarPhotoMaxRedirects,
	})(req, via)
}

func calendarPhotoNetguardPolicy() netguard.Policy {
	return netguard.Policy{
		Resolver:     calendarPhotoDNSResolver,
		Timeout:      calendarPhotoRequestTimeout,
		AllowedHosts: imagehost.AvatarHosts.Hosts(),
		AllowedPorts: []string{"443"},
		Schemes:      []string{"https"},
	}
}

func validateCalendarPhotoContentType(rawContentType string) (string, error) {
	contentType, _, err := mime.ParseMediaType(rawContentType)
	if err != nil {
		return "", fmt.Errorf("parse calendar photo content type: %w", err)
	}
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if _, ok := calendarPhotoAllowedContentTypes[contentType]; !ok {
		return "", fmt.Errorf("calendar photo content type %q is not allowed", contentType)
	}
	return contentType, nil
}

func normalizedCalendarPhotoHost(parsed *url.URL) string {
	return strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
}

func calendarPhotoURLHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return normalizedCalendarPhotoHost(parsed)
}
