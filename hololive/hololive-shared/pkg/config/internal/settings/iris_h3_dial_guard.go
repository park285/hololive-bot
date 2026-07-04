// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package settings

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

type settingsIrisH3Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

var settingsIrisH3DialResolver settingsIrisH3Resolver = net.DefaultResolver

func newSettingsIrisH3DialGuard(baseURL string, timeout time.Duration) func(net.IP) error {
	host, allowed, resolveErr := resolveSettingsIrisH3DialGuardIPs(baseURL, timeout)
	return func(ip net.IP) error {
		if ip == nil {
			return fmt.Errorf("iris h3 egress denied: nil dial ip")
		}
		if resolveErr != nil {
			return resolveErr
		}
		for _, candidate := range allowed {
			if candidate.Equal(ip) {
				return nil
			}
		}
		return fmt.Errorf("iris h3 egress denied: dial ip %s not in allowset derived from iris base url host %q", ip, host)
	}
}

func resolveSettingsIrisH3DialGuardIPs(baseURL string, timeout time.Duration) (string, []net.IP, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", nil, fmt.Errorf("parse iris base url for h3 egress guard: %w", err)
	}
	host := parsed.Hostname()
	if host == "" {
		return "", nil, fmt.Errorf("iris base url has no host for h3 egress guard")
	}

	if literal := net.ParseIP(host); literal != nil {
		return host, []net.IP{literal}, nil
	}

	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ipAddrs, err := settingsIrisH3DialResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", nil, fmt.Errorf("resolve iris base url host %q for h3 egress guard: %w", host, err)
	}
	if len(ipAddrs) == 0 {
		return "", nil, fmt.Errorf("iris base url host %q resolved to no addresses for h3 egress guard", host)
	}

	allowed := make([]net.IP, 0, len(ipAddrs))
	for _, ipAddr := range ipAddrs {
		allowed = append(allowed, ipAddr.IP)
	}
	return host, allowed, nil
}
