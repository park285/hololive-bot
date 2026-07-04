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
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/netguard"
)

var errBlockedLink = errors.New("parse link: blocked host")

func (c *LinkChecker) validateRequestTarget(ctx context.Context, rawURL string) error {
	_, err := linkCheckerNetguardPolicy(c.resolver, c.config.Timeout).ValidateURL(ctx, rawURL)
	return linkCheckerGuardError(err)
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
	return parsed, nil
}

func isBlockedLinkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errBlockedLink) {
		return true
	}
	return errors.Is(err, netguard.ErrBlockedIP) ||
		errors.Is(err, netguard.ErrHostNotAllowed) ||
		errors.Is(err, netguard.ErrUnsupportedScheme) ||
		strings.Contains(strings.ToLower(err.Error()), "unsupported scheme")
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
	clonedClient.Transport = netguard.GuardedTransport(clonedTransport, linkCheckerNetguardPolicy(resolver, timeout))
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

func withBlockedRedirectPolicy(client *http.Client, resolver hostResolver, timeout time.Duration) *http.Client {
	if client == nil {
		return nil
	}

	cloned := *client
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return linkCheckerGuardError(netguard.RedirectPolicy(netguard.RedirectConfig{
			Policy:        linkCheckerNetguardPolicy(resolver, timeout),
			CheckRedirect: client.CheckRedirect,
		})(req, via))
	}
	return &cloned
}

func linkCheckerNetguardPolicy(resolver hostResolver, timeout time.Duration) netguard.Policy {
	return netguard.Policy{
		Resolver: resolver,
		Timeout:  timeout,
		Schemes:  []string{"http", "https"},
	}
}

func linkCheckerGuardError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, netguard.ErrBlockedIP) || errors.Is(err, netguard.ErrHostNotAllowed) {
		return fmt.Errorf("%w: %w", errBlockedLink, err)
	}
	return err
}

func redactLinkForLog(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
