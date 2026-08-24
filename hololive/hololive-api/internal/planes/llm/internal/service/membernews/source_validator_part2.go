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
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"strings"
)

func loadXAllowlist(path string) ([]string, error) {
	// #nosec G304 -- allowlist 경로는 운영자가 제공한 config/env 입력이며 사용자 요청 데이터가 아니다.
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read allowlist file: %w", err)
	}

	var direct []string

	if err := jsonv2.Unmarshal(bytes, &direct); err == nil {
		return direct, nil
	}

	var wrapped struct {
		Accounts         []string `json:"accounts"`
		OfficialAccounts []string `json:"official_accounts"`
	}

	if err := jsonv2.Unmarshal(bytes, &wrapped); err != nil {
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
	trimmed = strings.TrimPrefix(trimmed, "http://x.com/") //nolint:revive // http:// 접두사 제거가 목적이라 https로 바꾸면 정규화가 동작하지 않는다.
	trimmed = strings.TrimPrefix(trimmed, "https://twitter.com/")
	trimmed = strings.TrimPrefix(trimmed, "http://twitter.com/") //nolint:revive // 위와 동일하게 레거시 http:// 계정 URL을 걷어내기 위한 리터럴이다.
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
