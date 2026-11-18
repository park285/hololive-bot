package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

type ClientHints struct {
	Brands          string `json:"brands,omitempty"`
	Mobile          bool   `json:"mobile"`
	Platform        string `json:"platform,omitempty"`
	PlatformVersion string `json:"platformVersion,omitempty"`
	Model           string `json:"model,omitempty"`
	Architecture    string `json:"architecture,omitempty"`
	Bitness         string `json:"bitness,omitempty"`
	FullVersionList string `json:"fullVersionList,omitempty"`
}

func parseClientHints(c *gin.Context) ClientHints {
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

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}

	return s
}

func (ch *ClientHints) HasClientHints() bool {
	if ch == nil {
		return false
	}

	return ch.Platform != "" || ch.Model != "" || ch.PlatformVersion != ""
}

func (ch *ClientHints) Summary() string {
	if !ch.HasClientHints() {
		return ""
	}

	var parts []string

	if ch.Platform != "" {
		parts = append(parts, ch.platformSummary())
	}

	if device := ch.deviceSummary(); device != "" {
		parts = append(parts, device)
	}

	if ch.Mobile && ch.Model == "" {
		parts = append(parts, "[Mobile]")
	}

	return strings.Join(parts, " ")
}

func (ch *ClientHints) platformSummary() string {
	if ch.PlatformVersion == "" {
		return ch.Platform
	}

	majorVersion := clientHintMajorVersion(ch.PlatformVersion)
	if strings.EqualFold(ch.Platform, "Windows") {
		return "Windows " + translateWindowsVersion(majorVersion)
	}

	return ch.Platform + " " + majorVersion
}

func (ch *ClientHints) deviceSummary() string {
	if ch.Model != "" {
		return "(" + ch.Model + ")"
	}

	if ch.Architecture == "" {
		return ""
	}

	return formatArchitecture(ch.Architecture, ch.Bitness)
}

func clientHintMajorVersion(version string) string {
	if idx := strings.Index(version, "."); idx > 0 {
		return version[:idx]
	}

	return version
}

func translateWindowsVersion(majorVersion string) string {
	major := parseLeadingInt(majorVersion)

	switch {
	case major >= 13:
		return "11"
	case major >= 1 && major <= 10:
		return "10"
	case major == 0:
		return "8.1 or older"
	default:
		return majorVersion
	}
}

func parseLeadingInt(value string) int {
	var parsed int

	for _, ch := range value {
		if ch < '0' || ch > '9' {
			break
		}

		parsed = parsed*10 + int(ch-'0')
	}

	return parsed
}

func formatArchitecture(arch, bitness string) string {
	if bitness == "" {
		return arch
	}

	if strings.EqualFold(arch, clientHintArchX86) && bitness == "64" {
		return "x64"
	}

	if strings.EqualFold(arch, "arm") && bitness == "64" {
		return "arm64"
	}

	if strings.EqualFold(arch, clientHintArchX86) && bitness == "32" {
		return clientHintArchX86
	}

	return arch + bitness
}

const clientHintArchX86 = "x86"
