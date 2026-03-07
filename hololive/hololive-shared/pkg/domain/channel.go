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

package domain

// Channel: YouTube 채널의 상세 정보 (이름, 아이디, 구독자 수 등)
type Channel struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	EnglishName     *string `json:"english_name,omitempty"`
	Photo           *string `json:"photo,omitempty"`
	Twitter         *string `json:"twitter,omitempty"`
	VideoCount      *int    `json:"video_count,omitempty"`
	SubscriberCount *int    `json:"subscriber_count,omitempty"`
	Org             *string `json:"org,omitempty"`
	Suborg          *string `json:"suborg,omitempty"`
	Group           *string `json:"group,omitempty"`
}

// GetDisplayName: 채널의 표시 이름을 반환한다. (영문 이름이 있으면 우선 사용)
func (c *Channel) GetDisplayName() string {
	if c == nil {
		return ""
	}
	if c.EnglishName != nil && *c.EnglishName != "" {
		return *c.EnglishName
	}
	return c.Name
}

// IsHololive: 해당 채널이 Hololive 소속인지 확인합니다.
func (c *Channel) IsHololive() bool {
	if c == nil || c.Org == nil {
		return false
	}
	return *c.Org == "Hololive"
}

// HasPhoto: 채널 프로필 사진 URL이 존재하는지 확인합니다.
func (c *Channel) HasPhoto() bool {
	if c == nil {
		return false
	}
	return c.Photo != nil && *c.Photo != ""
}

// GetPhotoURL: 채널 프로필 사진의 URL을 반환한다. 없으면 빈 문자열을 반환합니다.
func (c *Channel) GetPhotoURL() string {
	if c == nil {
		return ""
	}
	if c.HasPhoto() {
		return *c.Photo
	}
	return ""
}
