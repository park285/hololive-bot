package app

import (
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
		return nil, fmt.Errorf("bot dependencies not initialized")
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
