package server

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// ClientHints: 브라우저 Client Hints 정보를 담는 구조체입니다.
// User-Agent 축소 정책에 따라 정확한 기기 정보를 얻으려면 Client Hints를 사용해야 합니다.
//
// 예시 (기존 UA vs Client Hints):
//   - User-Agent: "Mozilla/5.0 (Linux; Android 10; K) ..." (축소됨)
//   - Client Hints: Platform="Android", PlatformVersion="16.0.0", Model="SM-S928N"
type ClientHints struct {
	// Brands: 브라우저 브랜드 및 버전 목록 (Sec-CH-UA)
	Brands string `json:"brands,omitempty"`
	// Mobile: 모바일 기기 여부 (Sec-CH-UA-Mobile)
	Mobile bool `json:"mobile"`
	// Platform: 플랫폼 (Sec-CH-UA-Platform, 예: "Android", "Windows")
	Platform string `json:"platform,omitempty"`
	// PlatformVersion: 정확한 플랫폼 버전 (Sec-CH-UA-Platform-Version, 예: "16.0.0")
	PlatformVersion string `json:"platformVersion,omitempty"`
	// Model: 기기 모델명 (Sec-CH-UA-Model, 예: "SM-S928N")
	Model string `json:"model,omitempty"`
	// Architecture: CPU 아키텍처 (Sec-CH-UA-Arch, 예: "arm")
	Architecture string `json:"architecture,omitempty"`
	// Bitness: CPU 비트 수 (Sec-CH-UA-Bitness, 예: "64")
	Bitness string `json:"bitness,omitempty"`
	// FullVersionList: 전체 버전 목록 (Sec-CH-UA-Full-Version-List)
	FullVersionList string `json:"fullVersionList,omitempty"`
}

// ParseClientHints: HTTP 요청 헤더에서 Client Hints를 파싱합니다.
// 표준 Sec-CH-UA-* 헤더를 읽어 ClientHints 구조체로 반환합니다.
//
// 참고: 브라우저가 Client Hints를 지원하지 않거나 헤더가 없으면 빈 값이 반환됩니다.
func ParseClientHints(c *gin.Context) ClientHints {
	return ClientHints{
		Brands:          c.GetHeader("Sec-CH-UA"),
		Mobile:          c.GetHeader("Sec-CH-UA-Mobile") == "?1",
		Platform:        unquote(c.GetHeader("Sec-CH-UA-Platform")),
		PlatformVersion: unquote(c.GetHeader("Sec-CH-UA-Platform-Version")),
		Model:           unquote(c.GetHeader("Sec-CH-UA-Model")),
		Architecture:    unquote(c.GetHeader("Sec-CH-UA-Arch")),
		Bitness:         unquote(c.GetHeader("Sec-CH-UA-Bitness")),
		FullVersionList: c.GetHeader("Sec-CH-UA-Full-Version-List"),
	}
}

// unquote: Sec-CH-UA 헤더 값에서 따옴표를 제거합니다.
// 예: `"Android"` → `Android`
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// HasClientHints: Client Hints 정보가 있는지 확인합니다.
func (ch ClientHints) HasClientHints() bool {
	return ch.Platform != "" || ch.Model != "" || ch.PlatformVersion != ""
}

// Summary: Client Hints를 요약 문자열로 반환합니다.
// 로그 표시용으로 사람이 읽기 쉬운 형태로 반환합니다.
//
// 예시:
//   - "Android 16 (SM-S928N)"
//   - "Windows 11 x64"
//   - "macOS 14"
//
// Windows 특수 처리:
//   - platformVersion 13+ → Windows 11
//   - platformVersion 1-10 → Windows 10
//   - (Client Hints에서 Windows 버전은 NT 커널 버전이 아닌 마케팅 버전으로 매핑됨)
func (ch ClientHints) Summary() string {
	if !ch.HasClientHints() {
		return ""
	}

	var parts []string

	// 플랫폼 + 버전
	if ch.Platform != "" {
		platformStr := ch.Platform
		if ch.PlatformVersion != "" {
			// 주요 버전만 추출 (예: "16.0.0" → "16")
			majorVersion := ch.PlatformVersion
			if idx := strings.Index(ch.PlatformVersion, "."); idx > 0 {
				majorVersion = ch.PlatformVersion[:idx]
			}

			// Windows 특수 처리: platformVersion을 마케팅 버전으로 변환
			if strings.EqualFold(ch.Platform, "Windows") {
				windowsVersion := translateWindowsVersion(majorVersion)
				platformStr = "Windows " + windowsVersion
			} else {
				platformStr += " " + majorVersion
			}
		}
		parts = append(parts, platformStr)
	}

	// 모델명 (모바일) 또는 아키텍처 (데스크톱)
	if ch.Model != "" {
		parts = append(parts, "("+ch.Model+")")
	} else if ch.Architecture != "" {
		arch := formatArchitecture(ch.Architecture, ch.Bitness)
		parts = append(parts, arch)
	}

	// 모바일 표시 (모델이 없는 경우)
	if ch.Mobile && ch.Model == "" {
		parts = append(parts, "[Mobile]")
	}

	return strings.Join(parts, " ")
}

// translateWindowsVersion: Windows Client Hints platformVersion을 마케팅 버전으로 변환합니다.
//
// Client Hints에서 Windows는 다음과 같이 버전을 보고합니다:
//   - Windows 11: platformVersion = "13.0.0", "14.0.0", "15.0.0" 등 (빌드 22000+)
//   - Windows 10: platformVersion = "1.0.0" ~ "10.0.0"
//   - Windows 8.1: platformVersion = "0.3.0"
//   - Windows 8: platformVersion = "0.2.0"
//   - Windows 7: platformVersion = "0.1.0"
//
// 참고: https://learn.microsoft.com/en-us/microsoft-edge/web-platform/how-to-detect-win11
func translateWindowsVersion(majorVersion string) string {
	// 숫자로 변환
	var major int
	for _, ch := range majorVersion {
		if ch >= '0' && ch <= '9' {
			major = major*10 + int(ch-'0')
		} else {
			break
		}
	}

	switch {
	case major >= 13:
		return "11" // Windows 11
	case major >= 1 && major <= 10:
		return "10" // Windows 10
	case major == 0:
		return "8.1 or older" // Windows 8.1 이하
	default:
		return majorVersion // 알 수 없는 경우 원본 반환
	}
}

// formatArchitecture: 아키텍처와 비트 수를 사용자 친화적인 형식으로 변환합니다.
//
// 변환 예시:
//   - x86 + 64 → x64
//   - arm + 64 → arm64
//   - x86 + 32 → x86
//   - arm + "" → arm
func formatArchitecture(arch, bitness string) string {
	if bitness == "" {
		return arch
	}

	// x86 + 64비트 = x64 (일반적인 표기)
	if strings.EqualFold(arch, "x86") && bitness == "64" {
		return "x64"
	}

	// arm + 64비트 = arm64
	if strings.EqualFold(arch, "arm") && bitness == "64" {
		return "arm64"
	}

	// 32비트 x86은 그냥 x86
	if strings.EqualFold(arch, "x86") && bitness == "32" {
		return "x86"
	}

	// 그 외는 조합
	return arch + bitness
}

// ToLogFields: 로그에 포함할 필드 맵을 반환합니다.
// 값이 있는 필드만 포함됩니다.
// Windows의 경우 내부 버전(13, 14, 15)을 사용자 친화적 버전(10, 11)으로 변환합니다.
func (ch ClientHints) ToLogFields() map[string]any {
	fields := make(map[string]any)

	if ch.Platform != "" {
		fields["platform"] = ch.Platform
	}
	if ch.PlatformVersion != "" {
		// Windows는 내부 버전을 마케팅 버전으로 변환
		if strings.EqualFold(ch.Platform, "Windows") {
			majorVersion := ch.PlatformVersion
			if idx := strings.Index(ch.PlatformVersion, "."); idx > 0 {
				majorVersion = ch.PlatformVersion[:idx]
			}
			fields["platform_version"] = translateWindowsVersion(majorVersion)
		} else {
			// Android, macOS 등은 실제 OS 버전 그대로
			fields["platform_version"] = ch.PlatformVersion
		}
	}
	if ch.Model != "" {
		fields["device_model"] = ch.Model
	}
	if ch.Mobile {
		fields["mobile"] = true
	}
	if ch.Architecture != "" {
		fields["arch"] = ch.Architecture
	}

	return fields
}

// clientHintsToRequest: 서버가 브라우저에게 요청할 Client Hints 목록입니다.
// Accept-CH 헤더에 포함되어 브라우저가 High Entropy 값을 전송하도록 요청합니다.
const clientHintsToRequest = "Sec-CH-UA, Sec-CH-UA-Mobile, Sec-CH-UA-Platform, " +
	"Sec-CH-UA-Platform-Version, Sec-CH-UA-Model, Sec-CH-UA-Arch, Sec-CH-UA-Bitness, " +
	"Sec-CH-UA-Full-Version-List"

// ClientHintsMiddleware: 모든 응답에 Accept-CH 헤더를 추가하여
// 브라우저에게 Client Hints를 요청하는 미들웨어입니다.
//
// 브라우저는 이 헤더를 받은 후 다음 요청부터 Sec-CH-UA-* 헤더를 전송합니다.
// 첫 요청에서는 Client Hints가 없을 수 있으므로 폴백 처리가 필요합니다.
//
// 참고: https://developer.chrome.com/docs/privacy-sandbox/user-agent/
func ClientHintsMiddleware() func(c *gin.Context) {
	return func(c *gin.Context) {
		// Accept-CH: 브라우저에게 다음 요청에서 이 Client Hints를 보내달라고 요청
		c.Header("Accept-CH", clientHintsToRequest)

		// Critical-CH: 중요한 Client Hints (첫 요청에서도 필요)
		// 브라우저가 지원하면 페이지 재로드를 트리거할 수 있음
		c.Header("Critical-CH", "Sec-CH-UA-Platform, Sec-CH-UA-Mobile")

		// Permissions-Policy: Client Hints 수신 허용
		c.Header("Permissions-Policy", "ch-ua-platform-version=*, ch-ua-model=*, ch-ua-arch=*")

		c.Next()
	}
}
