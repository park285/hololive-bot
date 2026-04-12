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

package providers

import (
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

// YouTubeStack - YouTube 관련 서비스 묶음 (선택적 활성화)
type YouTubeStack struct {
	Service   youtube.Service
	Scheduler youtube.Scheduler
	StatsRepo *ytstats.StatsRepository
}

func (s *YouTubeStack) GetService() youtube.Service {
	if s == nil {
		return nil
	}
	return s.Service
}

func (s *YouTubeStack) GetScheduler() youtube.Scheduler {
	if s == nil {
		return nil
	}
	return s.Scheduler
}

func (s *YouTubeStack) GetStatsRepo() *ytstats.StatsRepository {
	if s == nil {
		return nil
	}
	return s.StatsRepo
}
