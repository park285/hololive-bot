package shortlink

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	shortlinkcontracts "github.com/kapu/hololive-shared/pkg/contracts/shortlink"
)

const youtubeVideoIDLength = 11

// YouTubeBuilder는 허용된 public HTTPS origin과 YouTube video ID로만 단축 링크를 만듭니다.
type YouTubeBuilder struct {
	origin string
}

// NewYouTubeBuilder는 빈 값을 비활성 구성으로 허용하고 나머지는 origin 형태로 제한합니다.
func NewYouTubeBuilder(rawOrigin string) (YouTubeBuilder, error) {
	origin := strings.TrimSpace(rawOrigin)
	if origin == "" {
		return YouTubeBuilder{}, nil
	}

	parsed, err := url.ParseRequestURI(origin)
	if err != nil {
		return YouTubeBuilder{}, fmt.Errorf("parse origin: %w", err)
	}

	if err := validateYouTubeOriginAuthority(parsed); err != nil {
		return YouTubeBuilder{}, fmt.Errorf("validate youtube origin authority: %w", err)
	}

	if err := validateYouTubeOriginSuffix(parsed); err != nil {
		return YouTubeBuilder{}, fmt.Errorf("validate youtube origin suffix: %w", err)
	}

	parsed.Path = ""
	parsed.RawPath = ""

	return YouTubeBuilder{origin: strings.TrimSuffix(parsed.String(), "/")}, nil
}

func validateYouTubeOriginAuthority(parsed *url.URL) error {
	if parsed.Scheme != "https" {
		return errors.New("origin must use https")
	}

	if parsed.Hostname() == "" {
		return errors.New("origin host is required")
	}

	if parsed.User != nil {
		return errors.New("origin user info is not allowed")
	}

	return nil
}

func validateYouTubeOriginSuffix(parsed *url.URL) error {
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" {
		return errors.New("origin path is not allowed")
	}

	if parsed.RawQuery != "" || parsed.ForceQuery {
		return errors.New("origin query is not allowed")
	}

	if parsed.Fragment != "" {
		return errors.New("origin fragment is not allowed")
	}

	return nil
}

// Enabled는 단축 링크 origin이 설정되었는지 반환합니다.
func (b YouTubeBuilder) Enabled() bool {
	return b.origin != ""
}

// URL은 유효한 YouTube video ID에 대해서만 고정 목적지 단축 링크를 반환합니다.
func (b YouTubeBuilder) URL(videoID string) (string, bool) {
	videoID = strings.TrimSpace(videoID)
	if !b.Enabled() || !ValidYouTubeVideoID(videoID) {
		return "", false
	}

	return b.origin + shortlinkcontracts.YouTubePathPrefix + videoID, true
}

// ValidYouTubeVideoID는 YouTube의 11-byte URL-safe ID 형식만 허용합니다.
func ValidYouTubeVideoID(videoID string) bool {
	if len(videoID) != youtubeVideoIDLength {
		return false
	}

	for i := range len(videoID) {
		if !validYouTubeVideoIDByte(videoID[i]) {
			return false
		}
	}

	return true
}

func validYouTubeVideoIDByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '-' || value == '_'
}
