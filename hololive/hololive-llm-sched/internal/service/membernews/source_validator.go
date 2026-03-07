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
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var urlPattern = regexp.MustCompile(`https?://[^\s)]+`)

// SourceValidator: 도메인/x 계정 검증기.
type SourceValidator struct {
	officialDomains map[string]struct{}
	mediaDomains    map[string]struct{}
	xAllowlist      map[string]struct{}
	ytChannelIDs    map[string]struct{}
	ytHandles       map[string]struct{}
	logger          *slog.Logger
}

// NewSourceValidator: 출처 검증기 생성.
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

// ValidateSourceURL: URL 파싱 + 도메인/계정 검증 + 신뢰도 등급 판정을 수행합니다.
func (v *SourceValidator) ValidateSourceURL(rawURL string) (model.SourceTier, string, error) {
	if v == nil {
		return model.SourceTierCommunity, "", fmt.Errorf("source validator is nil")
	}

	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return model.SourceTierCommunity, "", fmt.Errorf("source url is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return model.SourceTierCommunity, "", fmt.Errorf("parse source url: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return model.SourceTierCommunity, "", fmt.Errorf("unsupported source url scheme: %s", parsed.Scheme)
	}

	host := normalizeHost(parsed.Hostname())
	if host == "" {
		return model.SourceTierCommunity, "", fmt.Errorf("source host is empty")
	}

	if host == "x.com" || host == "twitter.com" {
		account := extractXAccount(parsed.Path)
		if account == "" {
			return model.SourceTierCommunity, "", fmt.Errorf("x.com account not found")
		}
		if !v.isAllowedXAccount(account) {
			return model.SourceTierCommunity, "", fmt.Errorf("x.com account not in allowlist: %s", account)
		}
		return model.SourceTierOfficial, parsed.String(), nil
	}

	if isYouTubeHost(host) {
		return v.classifyYouTubeSource(parsed)
	}

	if containsHost(v.officialDomains, host) {
		return model.SourceTierOfficial, parsed.String(), nil
	}
	if containsHost(v.mediaDomains, host) {
		return model.SourceTierMedia, parsed.String(), nil
	}

	return model.SourceTierCommunity, parsed.String(), nil
}

// HasCorroboration: 본문 내 URL 중 official/media 출처가 하나라도 있으면 true.
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
		return model.SourceTierCommunity, "", fmt.Errorf("youtube url is nil")
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) >= 2 && segments[0] == "channel" {
		channelID := strings.TrimSpace(segments[1])
		if channelID != "" && v.isAllowedYouTubeChannelID(channelID) {
			return model.SourceTierOfficial, parsed.String(), nil
		}
		return model.SourceTierCommunity, parsed.String(), nil
	}

	if len(segments) >= 1 {
		first := strings.TrimSpace(segments[0])
		switch {
		case strings.HasPrefix(first, "@"):
			handle := strings.TrimPrefix(first, "@")
			if v.isAllowedYouTubeHandle(handle) {
				return model.SourceTierOfficial, parsed.String(), nil
			}
			return model.SourceTierCommunity, parsed.String(), nil
		case (first == "user" || first == "c") && len(segments) >= 2:
			if v.isAllowedYouTubeHandle(segments[1]) {
				return model.SourceTierOfficial, parsed.String(), nil
			}
			return model.SourceTierCommunity, parsed.String(), nil
		}
	}

	// watch / shorts / live / youtu.be — 채널 식별 불가 → community
	// SSOT: youtube.com(공식 채널)만 official, 동영상 링크는 채널 특정 불가
	return model.SourceTierCommunity, parsed.String(), nil
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

func loadXAllowlist(path string) ([]string, error) {
	// #nosec G304 -- allowlist path is operator-provided config/env input, not user request data.
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read allowlist file: %w", err)
	}

	var direct []string
	if err := json.Unmarshal(bytes, &direct); err == nil {
		return direct, nil
	}

	var wrapped struct {
		Accounts         []string `json:"accounts"`
		OfficialAccounts []string `json:"official_accounts"`
	}
	if err := json.Unmarshal(bytes, &wrapped); err != nil {
		return nil, fmt.Errorf("unmarshal allowlist: %w", err)
	}

	if len(wrapped.Accounts) > 0 {
		return wrapped.Accounts, nil
	}
	return wrapped.OfficialAccounts, nil
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "www.")
	return host
}

func isYouTubeHost(host string) bool {
	switch host {
	case "youtube.com", "m.youtube.com", "youtu.be":
		return true
	default:
		return false
	}
}

func containsHost(domainSet map[string]struct{}, host string) bool {
	for domain := range domainSet {
		d := normalizeHost(domain)
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func normalizeXAccount(raw string) string {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	trimmed = strings.TrimPrefix(trimmed, "@")
	trimmed = strings.TrimPrefix(trimmed, "https://x.com/")
	trimmed = strings.TrimPrefix(trimmed, "http://x.com/")
	trimmed = strings.TrimPrefix(trimmed, "https://twitter.com/")
	trimmed = strings.TrimPrefix(trimmed, "http://twitter.com/")
	trimmed = strings.Trim(trimmed, "/")
	return trimmed
}

func extractXAccount(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 {
		return ""
	}
	account := normalizeXAccount(segments[0])
	if account == "" {
		return ""
	}
	if account == "home" || account == "explore" || account == "i" || account == "search" {
		return ""
	}
	return account
}
