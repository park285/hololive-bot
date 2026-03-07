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

package twitch

import (
	"fmt"
	"strings"
	"time"
)

// TokenResponse: OAuth 토큰 응답
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// StreamsResponse: GET /streams 응답
type StreamsResponse struct {
	Data       []StreamData `json:"data"`
	Pagination Pagination   `json:"pagination"`
}

// StreamData: 개별 스트림 데이터
type StreamData struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	UserLogin    string    `json:"user_login"`
	UserName     string    `json:"user_name"`
	GameID       string    `json:"game_id"`
	GameName     string    `json:"game_name"`
	Type         string    `json:"type"` // "live"
	Title        string    `json:"title"`
	ViewerCount  int       `json:"viewer_count"`
	StartedAt    time.Time `json:"started_at"`
	Language     string    `json:"language"`
	ThumbnailURL string    `json:"thumbnail_url"`
	TagIDs       []string  `json:"tag_ids"`
	Tags         []string  `json:"tags"`
	IsMature     bool      `json:"is_mature"`
}

// Pagination: 페이지네이션 정보
type Pagination struct {
	Cursor string `json:"cursor"`
}

// IsLive: 스트림이 현재 라이브인지 확인
func (s *StreamData) IsLive() bool {
	return s.Type == "live"
}

// GetThumbnailURL: 썸네일 URL을 지정 크기로 반환
func (s *StreamData) GetThumbnailURL(width, height int) string {
	// placeholder 치환: {width}x{height}
	return strings.ReplaceAll(
		strings.ReplaceAll(s.ThumbnailURL, "{width}", fmt.Sprintf("%d", width)),
		"{height}", fmt.Sprintf("%d", height),
	)
}
