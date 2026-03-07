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

// Package scraper: YouTube HTML 스크래핑을 통해 채널 정보를 추출하는 패키지
// YouTube Data API v3 quota 절약을 위한 대안으로 사용됨
package scraper

// ChannelStats: 채널 통계 정보 (aboutChannelViewModel에서 추출)
type ChannelStats struct {
	ChannelID       string `json:"channelId"`
	SubscriberCount int64  `json:"subscriberCount"`
	ViewCount       int64  `json:"viewCount"`
	VideoCount      int64  `json:"videoCount"`
	JoinedDate      int64  `json:"joinedDate"` // Unix timestamp
	Description     string `json:"description"`
	Country         string `json:"country"`
	Handle          string `json:"handle"`
}

// ChannelSnippet: 채널 프로필 정보 (pageHeaderRenderer에서 추출)
type ChannelSnippet struct {
	Avatar []Thumbnail `json:"avatar"`
	Banner []Thumbnail `json:"banner"`
}

// Thumbnail: 이미지 정보
type Thumbnail struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// UpcomingEvent: 예정/라이브 방송 정보
type UpcomingEvent struct {
	VideoID       string      `json:"videoId"`
	Title         string      `json:"title"`
	Thumbnail     []Thumbnail `json:"thumbnail"`
	Status        string      `json:"status"` // "LIVE", "UPCOMING", "DEFAULT"
	StartTime     *int64      `json:"startTime,omitempty"`
	ViewCountText string      `json:"viewCountText"`
	ChannelTitle  string      `json:"channelTitle"`
}

// Video: 비디오 정보 (recent, popular에서 사용)
type Video struct {
	VideoID       string      `json:"videoId"`
	Title         string      `json:"title"`
	Thumbnail     []Thumbnail `json:"thumbnail"`
	ViewCount     int64       `json:"viewCount"`
	PublishedText string      `json:"publishedText"` // "2 days ago" 등
	Duration      string      `json:"duration"`      // "12:34" 형식
	ChannelID     string      `json:"channelId"`
	ChannelTitle  string      `json:"channelTitle"`
	ChannelHandle string      `json:"channelHandle"`
}

// CommunityPost: 커뮤니티 포스트 정보
type CommunityPost struct {
	PostID        string      `json:"postId"`
	AuthorID      string      `json:"authorId"`
	AuthorName    string      `json:"authorName"`
	AuthorPhoto   []Thumbnail `json:"authorPhoto"`
	ContentText   string      `json:"contentText"`
	PublishedText string      `json:"publishedText"`
	LikeCount     int64       `json:"likeCount"`
	CommentCount  int64       `json:"commentCount"`
	Images        []Thumbnail `json:"images,omitempty"`
	VideoID       string      `json:"videoId,omitempty"` // 첨부 비디오
}

// Playlist: 플레이리스트 정보
type Playlist struct {
	PlaylistID   string      `json:"playlistId"`
	Title        string      `json:"title"`
	Thumbnail    []Thumbnail `json:"thumbnail"`
	VideoCount   int64       `json:"videoCount"`
	ChannelID    string      `json:"channelId"`
	ChannelTitle string      `json:"channelTitle"`
}

// Short: 쇼츠 비디오 정보
type Short struct {
	VideoID   string      `json:"videoId"`
	Title     string      `json:"title"`
	Thumbnail []Thumbnail `json:"thumbnail"`
	ViewCount int64       `json:"viewCount"`
}
