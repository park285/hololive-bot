package render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"slices"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/schedulecard"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/sharedcache"
	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicscope"
)

const (
	scheduleCardFormat       = schedulecard.Format3x4
	scheduleCardCacheLimit   = 16
	scheduleCardCacheVersion = "v1"
	scheduleCardCacheTTL     = 7 * 24 * time.Hour
)

type scheduleCardKey struct {
	posterID      int64
	extractedAtNS int64
	changedAtNS   int64
	editedAtNS    int64
	changes       int
	format        schedulecard.Format
	memberName    string
	photo         string
}

func (k scheduleCardKey) String() string {
	return fmt.Sprintf("%d:%d:%d:%d:%d:%s:%q:%q", k.posterID, k.extractedAtNS, k.changedAtNS, k.editedAtNS, k.changes, k.format, k.memberName, k.photo)
}

func (k scheduleCardKey) shared() (key, hash string) {
	sum := sha256.Sum256([]byte(k.String()))

	return fmt.Sprintf("bot:schedule:card:%s:%d:%s", scheduleCardCacheVersion, k.posterID, k.format), hex.EncodeToString(sum[:])
}

func newScheduleCardKey(member *domain.Member, schedule *scheduleposter.PublishedSchedule, format schedulecard.Format) scheduleCardKey {
	key := scheduleCardKey{
		posterID: schedule.PosterID, extractedAtNS: schedule.ExtractedAt.UnixNano(), changes: len(schedule.Changes), format: format,
		memberName: member.DisplayName(), photo: member.Photo,
	}

	for i := range schedule.Rows {
		if edited := schedule.Rows[i].EditedAt; !edited.IsZero() {
			key.editedAtNS = max(key.editedAtNS, edited.UnixNano())
		}
	}

	for i := range schedule.Changes {
		key.changedAtNS = max(key.changedAtNS, schedule.Changes[i].RecordedAt.UnixNano())

		if edited := schedule.Changes[i].EditedAt; !edited.IsZero() {
			key.editedAtNS = max(key.editedAtNS, edited.UnixNano())
		}
	}

	return key
}

// ScheduleCardRenderer는 !스케쥴 카드를 그리고 (포스터, 행 추출 시각, 게시되는 변경의 마지막 기록 시각과 개수, 마지막 윤문 시각, 판형,
// 멤버 표시 이름과 사진)마다 한 번만 그린다. 그린 카드는 프로세스 메모리와 공유 cache에 함께 둔다.
type ScheduleCardRenderer struct {
	format  schedulecard.Format
	cache   *imageCache[scheduleCardKey]
	shared  sharedPNGCache
	flights singleflight.Group
	draw    func(schedulecard.Card, schedulecard.Format) ([]byte, error)
}

// NewScheduleCardRenderer의 store가 nil이면 카드를 프로세스 메모리에만 둔다.
func NewScheduleCardRenderer(store sharedcache.Store) *ScheduleCardRenderer {
	r := newScheduleCardRenderer(scheduleCardFormat, scheduleCardCacheLimit, schedulecard.Render)

	r.shared = newScheduleCardSharedCache(store)

	return r
}

func newScheduleCardSharedCache(store sharedcache.Store) sharedPNGCache {
	return sharedPNGCache{store: store, ttl: scheduleCardCacheTTL, event: "schedule_card_cache_failed"}
}

func newScheduleCardRenderer(format schedulecard.Format, cacheLimit int, draw func(schedulecard.Card, schedulecard.Format) ([]byte, error)) *ScheduleCardRenderer {
	return &ScheduleCardRenderer{format: format, cache: newImageCache[scheduleCardKey](cacheLimit), draw: draw}
}

func (r *ScheduleCardRenderer) RenderScheduleCard(ctx context.Context, member *domain.Member, schedule scheduleposter.PublishedSchedule) ([]byte, error) {
	if member == nil {
		return nil, errors.New("render schedule card: member is nil")
	}

	key := newScheduleCardKey(member, &schedule, r.format)
	if data, ok := r.cache.get(key); ok {
		return data, nil
	}

	// 공유 렌더는 첫 요청의 취소와 분리한다. 네트워크는 사진 예산(photoFetchBudget)이 끝을 정한다.
	workCtx := context.WithoutCancel(ctx)

	resultCh := r.flights.DoChan(key.String(), func() (any, error) {
		var data []byte

		// DoChan의 goroutine panic은 호출자의 복구 경계를 벗어나므로 공유 렌더 안에서 오류로 돌려준다.
		err := panicguard.RunE(slog.Default(), panicscope.BackgroundTask, "schedule-card-render", func() error {
			var renderErr error

			data, renderErr = r.renderAndStore(workCtx, key, member, schedule)

			return renderErr
		})
		if err != nil {
			return nil, fmt.Errorf("run shared schedule card render: %w", err)
		}

		return data, nil
	})

	var result singleflight.Result

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for schedule card: %w", ctx.Err())
	case result = <-resultCh:
	}

	if result.Err != nil {
		return nil, fmt.Errorf("render schedule card: %w", result.Err)
	}

	data, ok := result.Val.([]byte)
	if !ok || len(data) == 0 {
		return nil, errors.New("render schedule card: missing image data")
	}

	return bytes.Clone(data), nil
}

func (r *ScheduleCardRenderer) renderAndStore(ctx context.Context, key scheduleCardKey, member *domain.Member, schedule scheduleposter.PublishedSchedule) ([]byte, error) {
	if data, ok := r.cache.get(key); ok {
		return data, nil
	}

	sharedKey, hash := key.shared()
	if data, ok := r.shared.get(ctx, sharedKey, hash); ok {
		r.cache.put(key, data)

		return data, nil
	}

	var photoURLs []string

	if member.Photo != "" {
		photoURLs = []string{member.Photo}
	}

	photos, err := fetchPhotos(ctx, photoURLs, false)
	if err != nil {
		return nil, fmt.Errorf("fetch schedule card photo: %w", err)
	}

	photo := photos.photos[member.Photo]

	data, err := r.draw(newScheduleCard(member, schedule, photo), r.format)
	if err != nil {
		return nil, fmt.Errorf("draw schedule card: %w", err)
	}

	// 사진이 카드의 유일한 키비주얼이고 키가 한 주 내내 같으므로, 사진을 받지 못한 카드는 캐시하지 않고 다음 요청에서 다시 받는다.
	if photos.cachePolicy.memoryCacheable && (member.Photo == "" || photo != nil) {
		r.cache.put(key, data)
		r.shared.put(ctx, sharedKey, hash, data)
	}

	return data, nil
}

func newScheduleCard(member *domain.Member, schedule scheduleposter.PublishedSchedule, photo image.Image) schedulecard.Card {
	slots := schedule.Slots()
	items := make([]schedulecard.Item, len(slots))

	for i := range slots {
		slot := &slots[i]

		items[i] = schedulecard.Item{
			Day:       slot.Day,
			Start:     slot.Start,
			End:       slot.End,
			Converted: slot.SourceZone != "",
			FromPost:  fromPost(slot),
			Status:    schedulecard.Status(slot.Status),
			Title:     slot.TitleKo,
			With:      slot.WithKo,
			Place:     slot.PlaceKo,
			Tags:      slot.TagsKo,
			Note:      slot.NoteKo,
		}

		if slot.TimeChanged {
			setTimeChange(&items[i], slot)
		}
	}

	return schedulecard.Card{
		ChannelID:   member.ChannelID,
		MemberName:  member.DisplayName(),
		WeekStart:   schedule.WeekStart.AddDate(0, 0, schedule.RowsDayShift),
		WeekEnd:     schedule.WeekEnd.AddDate(0, 0, schedule.RowsDayShift),
		ExtractedAt: schedule.UpdatedAt(),
		Items:       items,
		Photo:       photo,
	}
}

func setTimeChange(item *schedulecard.Item, slot *scheduleposter.Slot) {
	if slot.Day.Equal(slot.OrigDay) {
		item.PrevStart = slot.OrigStart
	} else {
		item.MovedFrom = slot.OrigDay
	}
}

func fromPost(slot *scheduleposter.Slot) bool {
	return slices.ContainsFunc(slot.Steps, func(step scheduleposter.Step) bool {
		return step.Kind == scheduleposter.ChangeAdded || step.Kind == scheduleposter.ChangeMoved || step.Kind == scheduleposter.ChangeOff
	})
}
