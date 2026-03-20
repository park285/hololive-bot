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

package app

import (
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
)

// NewBot: 설정된 의존성을 사용하여 새로운 Bot 인스턴스를 생성합니다.
func (c *Container) NewBot() (*bot.Bot, error) {
	if c.botDeps == nil {
		return nil, errors.New("bot dependencies not initialized")
	}

	b, err := bot.NewBot(c.botDeps)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot instance: %w", err)
	}

	return b, nil
}

// GetYouTubeScheduler: 유튜버 스케줄러 인스턴스를 반환합니다.
func (c *Container) GetYouTubeScheduler() youtube.Scheduler { return c.botDeps.Scheduler }

// GetMemberRepo: 멤버 정보 저장소(Repository)를 반환합니다.
func (c *Container) GetMemberRepo() *member.Repository { return c.botDeps.MemberRepo }

// GetMemberCache: 멤버 정보 캐시 서비스를 반환합니다.
func (c *Container) GetMemberCache() *member.Cache { return c.botDeps.MemberCache }

// GetAlarmService: 알림 서비스를 반환합니다.
func (c *Container) GetAlarmService() domain.AlarmCRUD { return c.botDeps.Alarm }

// GetCache: 전역 캐시 서비스를 반환합니다.
func (c *Container) GetCache() cache.Client { return c.botDeps.Cache }

// GetHolodexService: Holodex API 서비스를 반환합니다.
func (c *Container) GetHolodexService() domain.StreamProvider { return c.botDeps.Holodex }

// GetYouTubeService: YouTube API 서비스를 반환합니다.
func (c *Container) GetYouTubeService() youtube.Service { return c.botDeps.Service }

// GetActivityLogger: 활동 로그 기록 서비스를 반환합니다.
func (c *Container) GetActivityLogger() *activity.Logger { return c.botDeps.Activity }

// GetSettingsService: 봇 설정 관리 서비스를 반환합니다.
func (c *Container) GetSettingsService() settings.ReadWriter { return c.botDeps.Settings }

// GetACLService: 접근 제어(ACL) 서비스를 반환합니다.
func (c *Container) GetACLService() *acl.Service { return c.botDeps.ACL }
