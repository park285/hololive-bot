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

package chzzk

type OpenAPIResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
	Content T      `json:"content,omitempty"`
}

type LivesResponse struct {
	Data []LiveData `json:"data"`
	Page PageInfo   `json:"page"`
}

type LiveData struct {
	LiveID                int      `json:"liveId"`
	LiveTitle             string   `json:"liveTitle"`
	LiveThumbnailImageURL string   `json:"liveThumbnailImageUrl"`
	ConcurrentUserCount   int      `json:"concurrentUserCount"`
	OpenDate              string   `json:"openDate"`
	Adult                 bool     `json:"adult"`
	Tags                  []string `json:"tags"`
	CategoryType          string   `json:"categoryType"`
	LiveCategory          string   `json:"liveCategory"`
	LiveCategoryValue     string   `json:"liveCategoryValue"`
	ChannelID             string   `json:"channelId"`
	ChannelName           string   `json:"channelName"`
	ChannelImageURL       string   `json:"channelImageUrl"`
}

type PageInfo struct {
	Next string `json:"next,omitempty"`
}

type ChannelsResponse struct {
	Data []ChannelData `json:"data"`
}

type ChannelData struct {
	ChannelID       string `json:"channelId"`
	ChannelName     string `json:"channelName"`
	ChannelImageURL string `json:"channelImageUrl"`
	FollowerCount   int    `json:"followerCount"`
	VerifiedMark    bool   `json:"verifiedMark"`
}
