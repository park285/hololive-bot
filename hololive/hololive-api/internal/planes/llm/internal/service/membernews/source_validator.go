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

package membernews

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var urlPattern = regexp.MustCompile(`https?://[^\s)]+`)

type SourceValidator struct {
	officialDomains map[string]struct{}
	mediaDomains    map[string]struct{}
	xAllowlist      map[string]struct{}
	ytChannelIDs    map[string]struct{}
	ytHandles       map[string]struct{}
	logger          *slog.Logger
}

func NewSourceValidator(
	xAllowlistPath string,
	membersData domain.MemberDataProvider,
	logger *slog.Logger,
) (*SourceValidator, error) {
	if logger == nil {
		logger = slog.Default()
	}

	validator := &SourceValidator{
		officialDomains: defaultOfficialDomains(),
		mediaDomains:    defaultMediaDomains(),
		xAllowlist:      make(map[string]struct{}),
		ytChannelIDs:    make(map[string]struct{}),
		ytHandles:       make(map[string]struct{}),
		logger:          logger,
	}

	validator.seedOfficialYouTubeAllowlist(membersData)

	if strings.TrimSpace(xAllowlistPath) == "" {
		return validator, nil
	}

	accounts, err := loadXAllowlist(xAllowlistPath)
	if err != nil {
		return nil, fmt.Errorf("load x allowlist: %w", err)
	}

	for _, account := range accounts {
		normalized := normalizeXAccount(account)
		if normalized == "" {
			continue
		}

		validator.xAllowlist[normalized] = struct{}{}
		validator.ytHandles[normalized] = struct{}{}
	}

	return validator, nil
}

func (v *SourceValidator) ValidateSourceURL(rawURL string) (model.SourceTier, string, error) {
	if v == nil {
		return model.SourceTierCommunity, "", errors.New("source validator is nil")
	}

	parsed, host, err := parseSourceURL(rawURL)
	if err != nil {
		return model.SourceTierCommunity, "", fmt.Errorf("parse source URL: %w", err)
	}

	out1, out2, err := v.classifySourceHost(host, parsed)
	if err != nil {
		return out1, out2, fmt.Errorf("classify source host: %w", err)
	}

	return out1, out2, nil
}

func parseSourceURL(rawURL string) (*url.URL, string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, "", errors.New("source url is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, "", fmt.Errorf("parse source url: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, "", fmt.Errorf("unsupported source url scheme: %s", parsed.Scheme)
	}

	host := normalizeHost(parsed.Hostname())
	if host == "" {
		return nil, "", errors.New("source host is empty")
	}

	return parsed, host, nil
}

func (v *SourceValidator) classifySourceHost(host string, parsed *url.URL) (model.SourceTier, string, error) {
	if isXHost(host) {
		tier, sourceURL, err := v.classifyXSourceURL(parsed)
		if err != nil {
			return tier, sourceURL, fmt.Errorf("%w", err)
		}

		return tier, sourceURL, nil
	}

	if isYouTubeHost(host) {
		tier, sourceURL, err := v.classifyYouTubeSourceURL(parsed)
		if err != nil {
			return tier, sourceURL, fmt.Errorf("%w", err)
		}

		return tier, sourceURL, nil
	}

	if containsHost(v.officialDomains, host) {
		return model.SourceTierOfficial, parsed.String(), nil
	}

	if containsHost(v.mediaDomains, host) {
		return model.SourceTierMedia, parsed.String(), nil
	}

	return model.SourceTierCommunity, parsed.String(), nil
}

func isXHost(host string) bool {
	return host == "x.com" || host == "twitter.com"
}

func (v *SourceValidator) classifyXSourceURL(parsed *url.URL) (model.SourceTier, string, error) {
	tier, sourceURL, err := v.classifyXSource(parsed)
	if err != nil {
		return tier, sourceURL, fmt.Errorf("classify x source: %w", err)
	}

	return tier, sourceURL, nil
}

func (v *SourceValidator) classifyYouTubeSourceURL(parsed *url.URL) (model.SourceTier, string, error) {
	tier, sourceURL, err := v.classifyYouTubeSource(parsed)
	if err != nil {
		return tier, sourceURL, fmt.Errorf("classify youtube source: %w", err)
	}

	return tier, sourceURL, nil
}

func (v *SourceValidator) classifyXSource(parsed *url.URL) (model.SourceTier, string, error) {
	account := extractXAccount(parsed.Path)
	if account == "" {
		return model.SourceTierCommunity, "", errors.New("x.com account not found")
	}

	if !v.isAllowedXAccount(account) {
		return model.SourceTierCommunity, "", fmt.Errorf("x.com account not in allowlist: %s", account)
	}

	return model.SourceTierOfficial, parsed.String(), nil
}

func (v *SourceValidator) HasCorroboration(text string) bool {
	if v == nil {
		return false
	}

	matches := urlPattern.FindAllString(text, -1)
	for _, link := range matches {
		tier, _, err := v.ValidateSourceURL(link)
		if err != nil {
			continue
		}

		if tier == model.SourceTierOfficial || tier == model.SourceTierMedia {
			return true
		}
	}

	return false
}

func (v *SourceValidator) classifyYouTubeSource(parsed *url.URL) (model.SourceTier, string, error) {
	if parsed == nil {
		return model.SourceTierCommunity, "", errors.New("youtube url is nil")
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if v.isAllowedYouTubeSourcePath(segments) {
		return model.SourceTierOfficial, parsed.String(), nil
	}

	// watch / shorts / live / youtu.be — 채널 식별 불가 → community
	// SSOT: youtube.com(공식 채널)만 official, 동영상 링크는 채널 특정 불가
	return model.SourceTierCommunity, parsed.String(), nil
}

func (v *SourceValidator) isAllowedYouTubeSourcePath(segments []string) bool {
	if len(segments) >= 2 && segments[0] == "channel" {
		channelID := strings.TrimSpace(segments[1])
		return channelID != "" && v.isAllowedYouTubeChannelID(channelID)
	}

	if len(segments) == 0 {
		return false
	}

	return v.isAllowedYouTubeHandlePath(segments)
}

func (v *SourceValidator) isAllowedYouTubeHandlePath(segments []string) bool {
	first := strings.TrimSpace(segments[0])
	switch {
	case strings.HasPrefix(first, "@"):
		return v.isAllowedYouTubeHandle(strings.TrimPrefix(first, "@"))
	case (first == "user" || first == "c") && len(segments) >= 2:
		return v.isAllowedYouTubeHandle(segments[1])
	default:
		return false
	}
}

func (v *SourceValidator) isAllowedXAccount(account string) bool {
	normalized := normalizeXAccount(account)
	if normalized == "" {
		return false
	}

	_, ok := v.xAllowlist[normalized]

	return ok
}

func (v *SourceValidator) isAllowedYouTubeChannelID(channelID string) bool {
	normalized := strings.TrimSpace(channelID)
	if normalized == "" {
		return false
	}

	_, ok := v.ytChannelIDs[normalized]

	return ok
}

func (v *SourceValidator) isAllowedYouTubeHandle(handle string) bool {
	normalized := normalizeXAccount(handle)
	if normalized == "" {
		return false
	}

	_, ok := v.ytHandles[normalized]

	return ok
}

func (v *SourceValidator) seedOfficialYouTubeAllowlist(membersData domain.MemberDataProvider) {
	if v == nil || membersData == nil {
		return
	}

	for _, channelID := range membersData.GetChannelIDs() {
		trimmed := strings.TrimSpace(channelID)
		if trimmed == "" {
			continue
		}

		v.ytChannelIDs[trimmed] = struct{}{}
	}
}

func defaultOfficialDomains() map[string]struct{} {
	return map[string]struct{}{
		"hololive.hololivepro.com": {},
		"hololivepro.com":          {},
		"cover-corp.com":           {},
	}
}

func defaultMediaDomains() map[string]struct{} {
	return map[string]struct{}{
		"prtimes.jp":        {},
		"oricon.co.jp":      {},
		"natalie.mu":        {},
		"famitsu.com":       {},
		"4gamer.net":        {},
		"animate.tv":        {},
		"dengekionline.com": {},
	}
}
