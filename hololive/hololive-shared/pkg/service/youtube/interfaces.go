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

package youtube

import "context"

// 외부 모듈은 구체 구현(serviceImpl)에 직접 의존하지 않고 이 인터페이스를 통해 주입받는다.
// 필요한 메서드만 남겨 이동·테스트·모킹 비용을 낮춘다.
type Service interface {
	SetScraperProxyEnabled(enabled bool) bool
	ScraperProxyEnabled() bool
	GetChannelStatistics(ctx context.Context, channelIDs []string) (map[string]*ChannelStats, error)
	GetRecentVideos(ctx context.Context, channelID string, maxResults int64) ([]string, error)
}

type Scheduler interface {
	Start(ctx context.Context)
	Stop()
}
