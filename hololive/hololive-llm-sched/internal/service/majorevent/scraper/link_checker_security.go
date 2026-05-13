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

package scraper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

var errBlockedLink = errors.New("parse link: blocked host")

func (c *LinkChecker) validateRequestTarget(ctx context.Context, rawURL string) error {
	parsed, err := parseAndValidateLink(rawURL)
	if err != nil {
		return err
	}
	return validateResolvedHost(ctx, c.resolver, c.config.Timeout, parsed)
}

func parseAndValidateLink(rawURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, fmt.Errorf("parse link: empty url")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse link: %w", err)
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("parse link: unsupported scheme %q", parsed.Scheme)
	}
	hostname := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if hostname == "" {
		return nil, fmt.Errorf("parse link: empty host")
	}
	if hostname == "localhost" {
		return nil, fmt.Errorf("%w %q", errBlockedLink, parsed.Host)
	}
	if ip := net.ParseIP(hostname); ip != nil && isPrivateOrInternalIP(ip) {
		return nil, fmt.Errorf("%w %q", errBlockedLink, parsed.Host)
	}
	return parsed, nil
}

func validateResolvedHost(ctx context.Context, resolver hostResolver, timeout time.Duration, parsed *url.URL) error {
	if resolver == nil || parsed == nil {
		return nil
	}

	ips, err := lookupResolvedIPs(ctx, resolver, timeout, parsed.Hostname())
	if err != nil {
		return err
	}
	if slices.ContainsFunc(ips, isPrivateOrInternalIP) {
		return fmt.Errorf("%w %q", errBlockedLink, parsed.Host)
	}
	return nil
}

func lookupResolvedIPs(ctx context.Context, resolver hostResolver, timeout time.Duration, host string) ([]net.IP, error) {
	if resolver == nil {
		return nil, nil
	}

	lookupCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		lookupCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	ips, err := resolver.LookupIP(lookupCtx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve link host: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve link host: no addresses for %q", host)
	}
	return ips, nil
}

func isBlockedLinkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errBlockedLink) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "unsupported scheme")
}

func withValidatedDialPolicy(client *http.Client, resolver hostResolver, timeout time.Duration) *http.Client {
	if client == nil {
		return nil
	}

	clonedTransport, ok := cloneHTTPTransport(client.Transport)
	if !ok {
		return client
	}

	clonedClient := *client
	baseDialContext := clonedTransport.DialContext
	if baseDialContext == nil {
		dialer := &net.Dialer{}
		baseDialContext = dialer.DialContext
	}
	clonedTransport.DialContext = validatedDialContext(baseDialContext, resolver, timeout)
	withValidatedDialTLSPolicy(clonedTransport, resolver, timeout)
	clonedClient.Transport = clonedTransport
	return &clonedClient
}

func cloneHTTPTransport(transport http.RoundTripper) (*http.Transport, bool) {
	if transport == nil {
		transport = http.DefaultTransport
	}

	baseTransport, ok := transport.(*http.Transport)
	if !ok {
		return nil, false
	}
	return baseTransport.Clone(), true
}

func validatedDialContext(
	baseDialContext func(context.Context, string, string) (net.Conn, error),
	resolver hostResolver,
	timeout time.Duration,
) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		resolvedAddr, err := resolveDialAddress(ctx, resolver, timeout, addr)
		if err != nil {
			return nil, err
		}
		return baseDialContext(ctx, network, resolvedAddr)
	}
}

func withValidatedDialTLSPolicy(transport *http.Transport, resolver hostResolver, timeout time.Duration) {
	if transport.DialTLSContext == nil {
		return
	}
	transport.DialTLSContext = validatedDialContext(transport.DialTLSContext, resolver, timeout)
}

func resolveDialAddress(ctx context.Context, resolver hostResolver, timeout time.Duration, addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("resolve dial target: split host/port: %w", err)
	}

	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrInternalIP(ip) {
			return "", fmt.Errorf("%w %q", errBlockedLink, addr)
		}
		return addr, nil
	}
	if resolver == nil {
		return addr, nil
	}

	ips, err := lookupResolvedIPs(ctx, resolver, timeout, host)
	if err != nil {
		return "", err
	}
	if slices.ContainsFunc(ips, isPrivateOrInternalIP) {
		return "", fmt.Errorf("%w %q", errBlockedLink, addr)
	}
	return net.JoinHostPort(ips[0].String(), port), nil
}

func withBlockedRedirectPolicy(client *http.Client, resolver hostResolver, timeout time.Duration) *http.Client {
	if client == nil {
		return nil
	}

	cloned := *client
	original := client.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := validateResolvedHost(req.Context(), resolver, timeout, req.URL); err != nil {
			return err
		}
		if original != nil {
			return original(req, via)
		}
		return nil
	}
	return &cloned
}

func isPrivateOrInternalIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

func redactLinkForLog(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
