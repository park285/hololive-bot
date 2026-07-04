package delivery

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const runtimeIrisH3DialGuardResolveTimeout = 5 * time.Second

func newRuntimeIrisH3DialGuard(resolveBaseURL func() (string, error)) func(net.IP) error {
	return func(ip net.IP) error {
		if ip == nil {
			return fmt.Errorf("iris h3 egress denied: nil dial ip")
		}
		host, allowed, err := runtimeIrisH3DialGuardAllowset(resolveBaseURL)
		if err != nil {
			return err
		}
		if ipInRuntimeAllowset(ip, allowed) {
			return nil
		}
		return fmt.Errorf("iris h3 egress denied: dial ip %s not in allowset derived from iris base url host %q", ip, host)
	}
}

func runtimeIrisH3DialGuardAllowset(resolveBaseURL func() (string, error)) (string, []net.IP, error) {
	if resolveBaseURL == nil {
		return "", nil, fmt.Errorf("iris h3 egress guard has no base url resolver")
	}
	baseURL, err := resolveBaseURL()
	if err != nil {
		return "", nil, fmt.Errorf("resolve iris base url for h3 egress guard: %w", err)
	}
	return resolveRuntimeIrisH3DialGuardIPs(baseURL)
}

func ipInRuntimeAllowset(ip net.IP, allowed []net.IP) bool {
	for _, candidate := range allowed {
		if candidate.Equal(ip) {
			return true
		}
	}
	return false
}

func resolveRuntimeIrisH3DialGuardIPs(baseURL string) (string, []net.IP, error) {
	host, err := runtimeHostFromIrisBaseURL(baseURL)
	if err != nil {
		return "", nil, err
	}
	if literal := net.ParseIP(host); literal != nil {
		return host, []net.IP{literal}, nil
	}
	// H3DialGuard 콜백(func(net.IP) error)에는 dial ctx가 전달되지 않으므로 여기서 자체
	// timeout ctx를 root한다. build ctx를 threading하면 build timeout 후 모든 dial이 거부된다.
	ctx, cancel := context.WithTimeout(context.Background(), runtimeIrisH3DialGuardResolveTimeout)
	defer cancel()
	ipAddrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", nil, fmt.Errorf("resolve iris base url host %q for h3 egress guard: %w", host, err)
	}
	if len(ipAddrs) == 0 {
		return "", nil, fmt.Errorf("iris base url host %q resolved to no addresses for h3 egress guard", host)
	}
	return host, runtimeIPAddrsToIPs(ipAddrs), nil
}

func runtimeHostFromIrisBaseURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("parse iris base url for h3 egress guard: %w", err)
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("iris base url has no host for h3 egress guard")
	}
	return host, nil
}

func runtimeIPAddrsToIPs(ipAddrs []net.IPAddr) []net.IP {
	allowed := make([]net.IP, 0, len(ipAddrs))
	for _, ipAddr := range ipAddrs {
		allowed = append(allowed, ipAddr.IP)
	}
	return allowed
}
