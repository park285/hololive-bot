package render

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	calendarPhotoRequestTimeout = 5 * time.Second
	calendarPhotoMaxRedirects   = 2
)

var calendarPhotoAllowedHosts = map[string]struct{}{
	"yt3.ggpht.com":             {},
	"yt3.googleusercontent.com": {},
}

var calendarPhotoAllowedContentTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

type calendarPhotoResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
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
		return &http.Transport{
			Proxy:       nil,
			DialContext: dialCalendarPhotoContext,
		}
	}
	transport := baseTransport.Clone()
	transport.Proxy = nil
	transport.DialContext = dialCalendarPhotoContext
	return transport
}

func validateCalendarPhotoURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse calendar photo url: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("calendar photo url scheme %q is not allowed", parsed.Scheme)
	}
	host := normalizedCalendarPhotoHost(parsed)
	if host == "" {
		return errors.New("calendar photo url host is empty")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return fmt.Errorf("calendar photo url port %q is not allowed", port)
	}
	if _, ok := calendarPhotoAllowedHosts[host]; !ok {
		return fmt.Errorf("calendar photo host %q is not allowed", host)
	}
	return nil
}

func checkCalendarPhotoRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > calendarPhotoMaxRedirects {
		return errors.New("calendar photo redirect limit exceeded")
	}
	if req == nil || req.URL == nil {
		return errors.New("calendar photo redirect target is empty")
	}
	return validateCalendarPhotoURL(req.URL.String())
}

func dialCalendarPhotoContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, addrs, err := resolveCalendarPhotoDialTarget(ctx, address)
	if err != nil {
		return nil, err
	}

	return dialResolvedCalendarPhotoAddrs(ctx, network, host, port, addrs)
}

func resolveCalendarPhotoDialTarget(ctx context.Context, address string) (host, port string, addrs []netip.Addr, err error) {
	host, port, err = net.SplitHostPort(address)
	if err != nil {
		return "", "", nil, fmt.Errorf("split calendar photo dial address: %w", err)
	}
	if err := validateCalendarPhotoDialPort(port); err != nil {
		return "", "", nil, err
	}

	addrs, err = resolveCalendarPhotoHost(ctx, host)
	if err != nil {
		return "", "", nil, err
	}
	if len(addrs) == 0 {
		return "", "", nil, fmt.Errorf("resolve calendar photo host %q: no addresses", host)
	}
	if err := validateCalendarPhotoResolvedIPs(host, addrs); err != nil {
		return "", "", nil, err
	}

	return host, port, addrs, nil
}

func validateCalendarPhotoDialPort(port string) error {
	if port != "443" {
		return fmt.Errorf("calendar photo dial port %q is not allowed", port)
	}
	return nil
}

func validateCalendarPhotoResolvedIPs(host string, addrs []netip.Addr) error {
	for _, addr := range addrs {
		if blockedCalendarPhotoIP(addr) {
			return fmt.Errorf("calendar photo host %q resolved to blocked IP %s", host, addr.Unmap())
		}
	}
	return nil
}

func dialResolvedCalendarPhotoAddrs(ctx context.Context, network, host, port string, addrs []netip.Addr) (net.Conn, error) {
	var lastErr error
	for _, addr := range addrs {
		conn, err := calendarPhotoNetworkDialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("dial calendar photo host %q: %w", host, lastErr)
}

func resolveCalendarPhotoHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}
	resolver := calendarPhotoDNSResolver
	if resolver == nil {
		return nil, errors.New("calendar photo resolver is nil")
	}
	addrs, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve calendar photo host %q: %w", host, err)
	}
	return addrs, nil
}

func blockedCalendarPhotoIP(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsUnspecified() ||
		addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast()
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
