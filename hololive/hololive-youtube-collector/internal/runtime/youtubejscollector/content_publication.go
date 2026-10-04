package youtubejscollector

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

// ContentPublicationMaxCalls는 content job 하나가 video_list 공개 근거를 얻으려고 추가로 보내는 /v1/video_live_check RPC 상한입니다.
// 호출마다 RPC limiter를 한 번 기다리므로 fleet 호출 상한을 우회하지 않으며, registry의 job 최대 upstream 호출 수에 더해
// 수집 시간 예산을 계산합니다. 목록 RPC 하나에 영상별 호출을 숨기지 않습니다.
const ContentPublicationMaxCalls = 2

const (
	publicationCursorVersion = 1
	// 최근 조회 이력만 cursor에 보존한다. 목록 전체의 다음 위치를 따로 기록하므로
	// cache 상한 밖 항목도 순환 조회하며, checkpoint의 16KiB 상한을 넘지 않는다.
	maxPublicationCursorEntries = 48
	maxPublicationCursorBytes   = 16384
)

// ContentCursorReader는 같은 provider·kind·subject의 가장 최근 checkpoint cursor를 읽습니다.
// 저장된 cursor가 없으면 nil을 반환합니다. *sourceobservation.Repository가 구현합니다.
type ContentCursorReader interface {
	LatestCheckpointCursor(ctx context.Context, provider contract.Provider, kind contract.ObservationKind, subjectKey string) (jsontext.Value, error)
}

// publicationCursor는 video_list checkpoint에 함께 저장하는 영상별 공개 근거 조회 이력입니다.
// 관측·checkpoint와 같은 publish tx로 저장되므로 수락된 관측과 어긋나지 않으며, collector 재시작·다른 인스턴스도 이어 씁니다.
// ScheduledFor는 이 cursor와 함께 수락된 video_list 관측의 slot입니다. 같은 slot 재시도는 이미 수락된 관측을 다시 만들지 않습니다.
type publicationCursor struct {
	Version      int                      `json:"version"`
	ScheduledFor time.Time                `json:"scheduled_for"`
	HeadVideoID  string                   `json:"head_video_id"`
	NextPosition int                      `json:"next_position"`
	Entries      []publicationCursorEntry `json:"entries"`
}

// publicationCursorEntry는 영상 하나의 마지막 조회 시도입니다. Publication이 nil이면 응답을 얻지 못한 시도라 근거가 아닙니다.
type publicationCursorEntry struct {
	VideoID     string                       `json:"video_id"`
	AttemptedAt time.Time                    `json:"attempted_at"`
	Publication *contract.VideoPublicationV1 `json:"publication,omitempty"`
}

type publicationCandidate struct {
	position   int
	prior      *publicationCursorEntry
	changed    bool
	entryIndex int
	distance   int
	newHead    bool
}

// enrichPublications는 새로 목록 앞에 들어온 항목을 먼저 조회하고, 나머지 전체 목록은 저장된 위치부터 순환합니다.
// 근거 cache가 가득 차도 뒤쪽 항목을 영구 제외하지 않습니다. 호출마다 기존 limiter를 기다리며 한 job에서
// 최대 ContentPublicationMaxCalls번만 요청합니다. 실패한 시도도 위치를 전진시키고 같은 job의 추가 조회를 멈춥니다.
// 부모 수집 문맥 자체가 끝났을 때만 관측을 포기하고, enrichCtx가 끝나면 그때까지 받은 근거를 보존합니다.
func (r *ContentRunner) enrichPublications(
	enrichCtx context.Context,
	ctx context.Context,
	channelID string,
	items []youtubejs.ContentItem,
	prior storedPublicationCursor,
	maxSuccessResponseBytes int,
) ([]*contract.VideoPublicationV1, publicationCursor, error) {
	window := len(items)
	entries := make([]publicationCursorEntry, 0, min(window, maxPublicationCursorEntries+ContentPublicationMaxCalls))
	publications := make([]*contract.VideoPublicationV1, window)
	cursor, newPrefix := startPublicationCursor(items, &prior)

	candidates, entries := publicationCandidates(items, &prior, cursor.NextPosition, newPrefix, entries, publications)
	slices.SortStableFunc(candidates, comparePublicationCandidates)

	batch := candidates[:min(len(candidates), ContentPublicationMaxCalls)]
	for i := 0; i < len(batch) && enrichCtx.Err() == nil; i++ {
		candidate := &batch[i]

		entry, err := r.attemptPublication(enrichCtx, ctx, channelID, items[candidate.position].VideoID, maxSuccessResponseBytes)
		if err != nil {
			return nil, publicationCursor{}, err
		}

		publications[candidate.position] = entry.Publication

		if candidate.entryIndex < 0 {
			entries = append(entries, entry)
		} else {
			entries[candidate.entryIndex] = entry
		}

		cursor.NextPosition = (candidate.position + 1) % window

		if entry.Publication == nil {
			break
		}
	}

	cursor.Entries = trimPublicationEntries(entries)

	return publications, cursor, nil
}

// startPublicationCursor는 현재 목록의 head와 저장된 순환 위치로 새 cursor를 시작하고,
// 직전 head 앞에 새로 들어온 항목 수를 반환합니다. 직전 head가 목록에 없으면 새 head 구간은 없습니다.
func startPublicationCursor(items []youtubejs.ContentItem, prior *storedPublicationCursor) (publicationCursor, int) {
	cursor := publicationCursor{Version: publicationCursorVersion}

	if len(items) == 0 {
		return cursor, 0
	}

	cursor.HeadVideoID = items[0].VideoID
	cursor.NextPosition = prior.nextPosition % len(items)

	if prior.headVideoID == "" {
		return cursor, 0
	}

	for i := range items {
		if items[i].VideoID == prior.headVideoID {
			return cursor, i
		}
	}

	return cursor, 0
}

// publicationCandidates는 재사용할 수 있는 저장 근거를 publications에 채우고 나머지 항목을 조회 후보로 반환합니다.
// 저장된 이력이 있는 항목은 entries에 옮겨 담아 조회 결과가 같은 자리를 갱신하게 합니다.
func publicationCandidates(
	items []youtubejs.ContentItem,
	prior *storedPublicationCursor,
	nextPosition int,
	newPrefix int,
	entries []publicationCursorEntry,
	publications []*contract.VideoPublicationV1,
) ([]publicationCandidate, []publicationCursorEntry) {
	window := len(items)
	candidates := make([]publicationCandidate, 0, window)

	for i := range items {
		candidate := publicationCandidate{
			position: i, entryIndex: -1,
			distance: (i - nextPosition + window) % window,
			newHead:  i < newPrefix,
		}

		if previous, ok := prior.entries[items[i].VideoID]; ok {
			candidate.entryIndex = len(entries)
			entries = append(entries, previous)
			candidate.prior = &entries[candidate.entryIndex]

			if reusablePublication(previous.Publication, &items[i]) {
				publications[i] = previous.Publication

				continue
			}

			candidate.changed = previous.Publication != nil && previous.Publication.Status != contract.VideoPublicationUnresolved
			if previous.Publication != nil && !candidate.changed {
				publications[i] = previous.Publication
			}
		}

		candidates = append(candidates, candidate)
	}

	return candidates, entries
}

// attemptPublication은 후보 하나를 조회해 cursor 이력 항목을 만듭니다. 응답을 얻지 못한 시도는 Publication이 nil인
// 항목으로 기록해 순환 위치를 전진시킵니다. 부모 문맥이 끝났거나 근거 조회 실패로 강등할 수 없는 오류만 반환합니다.
func (r *ContentRunner) attemptPublication(
	enrichCtx context.Context,
	ctx context.Context,
	channelID string,
	videoID string,
	maxSuccessResponseBytes int,
) (publicationCursorEntry, error) {
	publication, err := r.fetchPublication(enrichCtx, channelID, videoID, maxSuccessResponseBytes)
	if err == nil {
		return publicationCursorEntry{VideoID: videoID, AttemptedAt: publication.CheckedAt, Publication: &publication}, nil
	}

	if ctx.Err() != nil {
		return publicationCursorEntry{}, fmt.Errorf("fetch video publication: %w", ctx.Err())
	}

	// 근거 조회 시한이 끝났어도 설정·취소·소유권·내부 오류를 정상 목록으로 숨기지 않습니다.
	if !liveCheckRequestFailed(ctx, err) {
		return publicationCursorEntry{}, fmt.Errorf("fetch video publication: %w", err)
	}

	return publicationCursorEntry{VideoID: videoID, AttemptedAt: time.Now().UTC()}, nil
}

// reusablePublication은 저장된 결정적 근거가 현재 목록의 구조화된 예정 표시와 맞을 때만 재사용합니다.
// 예정 Premiere가 공개된 뒤나 공개 근거가 있던 항목이 예정 표시로 바뀌면 상태가 바뀐 항목으로 다시 조회합니다.
func reusablePublication(publication *contract.VideoPublicationV1, item *youtubejs.ContentItem) bool {
	if publication == nil {
		return false
	}

	switch publication.Status {
	case contract.VideoPublicationPublished:
		return !item.IsUpcoming
	case contract.VideoPublicationUpcomingPremiere:
		return item.IsUpcoming
	case contract.VideoPublicationUnresolved:
		return false
	}

	return false
}

// comparePublicationCandidates는 새 head 구간, 미조회·상태 변경, 이전 실패 순서로 처리합니다.
// 같은 우선순위에서는 순환 위치를 사용해 작은 cache가 큰 목록의 앞부분만 반복하지 않게 합니다.
func comparePublicationCandidates(left, right publicationCandidate) int {
	if left.newHead != right.newHead {
		if left.newHead {
			return -1
		}

		return 1
	}

	leftFresh := left.prior == nil || left.changed

	rightFresh := right.prior == nil || right.changed
	if leftFresh != rightFresh {
		if leftFresh {
			return -1
		}

		return 1
	}

	if !leftFresh {
		if order := left.prior.AttemptedAt.Compare(right.prior.AttemptedAt); order != 0 {
			return order
		}
	}

	return cmp.Compare(left.distance, right.distance)
}

func trimPublicationEntries(entries []publicationCursorEntry) []publicationCursorEntry {
	slices.SortFunc(entries, func(left, right publicationCursorEntry) int {
		if order := right.AttemptedAt.Compare(left.AttemptedAt); order != 0 {
			return order
		}

		return cmp.Compare(left.VideoID, right.VideoID)
	})

	return entries[:min(len(entries), maxPublicationCursorEntries)]
}

// fetchPublication은 영상 하나의 player 확인 RPC를 한 번 보내고 결과를 공개 근거로 분류합니다.
func (r *ContentRunner) fetchPublication(ctx context.Context, channelID, videoID string, maxSuccessResponseBytes int) (contract.VideoPublicationV1, error) {
	result, err := r.client.FetchVideoLiveCheck(ctx, youtubejs.VideoLiveCheckRequest{
		VideoID:                 videoID,
		MaxSuccessResponseBytes: maxSuccessResponseBytes,
	})
	if err == nil {
		err = validateLiveCheckSubject("video", videoID, result.VideoID)
	}

	if err != nil {
		return contract.VideoPublicationV1{}, err
	}

	return classifyPublication(&result, channelID, time.Now().UTC()), nil
}

// classifyPublication은 player 사실만으로 공개 근거를 만듭니다. 이 채널로 identity가 확인되지 않거나 가용성을 판정하지 못했거나
// 비공개인 응답, 대기 상태가 확인되지 않은 예정 영상, 예정 live 방송, 정확한 publishDate가 없거나 확인 시각보다 늦은 응답은
// UNRESOLVED입니다. 목록 문자열·관측 순서·처음 본 시각으로 시각을 추정하지 않습니다.
func classifyPublication(result *youtubejs.VideoLiveCheckResult, channelID string, checkedAt time.Time) contract.VideoPublicationV1 {
	unresolved := contract.VideoPublicationV1{Status: contract.VideoPublicationUnresolved, CheckedAt: checkedAt}

	if !result.IdentityConfirmed || result.ChannelID != channelID {
		return unresolved
	}

	if result.Availability == contract.VideoAvailabilityUnknown || result.Availability == contract.VideoAvailabilityPublicUnavailable {
		return unresolved
	}

	if result.IsUpcoming != nil && *result.IsUpcoming {
		if result.IsLiveContent == nil || *result.IsLiveContent || result.ScheduledAt == nil {
			return unresolved
		}

		return contract.VideoPublicationV1{
			Status: contract.VideoPublicationUpcomingPremiere, ScheduledFor: new(result.ScheduledAt.UTC()), CheckedAt: checkedAt,
		}
	}

	if result.PublishedAt == nil || result.PublishedAt.After(checkedAt) {
		return unresolved
	}

	return contract.VideoPublicationV1{
		Status: contract.VideoPublicationPublished, PublishedAt: new(result.PublishedAt.UTC()), CheckedAt: checkedAt,
	}
}

// storedPublicationCursor는 직전에 수락된 video_list checkpoint cursor를 영상별로 푼 결과입니다.
type storedPublicationCursor struct {
	scheduledFor time.Time
	entries      map[string]publicationCursorEntry
	headVideoID  string
	nextPosition int
}

// loadPublicationCursor는 현재 관측 계약의 durable cursor만 읽습니다. 이력이 없으면 첫 조회이며,
// 존재하지만 해석할 수 없는 cursor는 데이터 계약 오류입니다. 이를 이력 없음으로 숨기거나 자동 정산하지 않습니다.
func (r *ContentRunner) loadPublicationCursor(ctx context.Context, channelID string) (storedPublicationCursor, error) {
	raw, err := r.cursors.LatestCheckpointCursor(ctx, contract.ProviderYouTubeJS, contract.KindVideoList, channelID)
	if err != nil {
		return storedPublicationCursor{}, fmt.Errorf("latest checkpoint cursor: %w", err)
	}

	if len(raw) == 0 {
		return storedPublicationCursor{}, nil
	}

	var cursor publicationCursor

	if decodeErr := jsonv2.Unmarshal(raw, &cursor, jsonv2.RejectUnknownMembers(true)); decodeErr != nil {
		return storedPublicationCursor{}, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("decode publication cursor: %w", decodeErr))
	}

	if !validPublicationCursorShape(len(raw), &cursor) {
		return storedPublicationCursor{}, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "publication cursor shape is invalid")
	}

	entries, err := publicationCursorEntries(cursor.Entries, time.Now().UTC())
	if err != nil {
		return storedPublicationCursor{}, err
	}

	return storedPublicationCursor{
		scheduledFor: cursor.ScheduledFor, entries: entries,
		headVideoID: cursor.HeadVideoID, nextPosition: cursor.NextPosition,
	}, nil
}

// validPublicationCursorShape는 저장 크기·version·slot·순환 위치·head·이력 수가 현재 cursor 계약 안에 있는지 확인합니다.
func validPublicationCursorShape(rawSize int, cursor *publicationCursor) bool {
	return rawSize <= maxPublicationCursorBytes && cursor.Version == publicationCursorVersion &&
		!cursor.ScheduledFor.IsZero() && cursor.NextPosition >= 0 &&
		len(cursor.HeadVideoID) <= 128 && len(cursor.Entries) <= maxPublicationCursorEntries
}

// publicationCursorEntries는 저장된 조회 이력을 영상별로 풀고, 잘못되었거나 중복된 이력과 검증되지 않는 근거를 거부합니다.
func publicationCursorEntries(stored []publicationCursorEntry, now time.Time) (map[string]publicationCursorEntry, error) {
	entries := make(map[string]publicationCursorEntry, len(stored))

	for i := range stored {
		entry := stored[i]
		if entry.VideoID == "" || len(entry.VideoID) > 128 || entry.AttemptedAt.IsZero() || entry.AttemptedAt.After(now) {
			return nil, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "publication cursor entry is invalid")
		}

		if _, duplicate := entries[entry.VideoID]; duplicate {
			return nil, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "publication cursor repeats a video")
		}

		if entry.Publication != nil {
			if err := entry.Publication.Validate(now); err != nil {
				return nil, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("validate cached publication: %w", err))
			}
		}

		entries[entry.VideoID] = entry
	}

	return entries, nil
}

// marshalPublicationCursor는 최근 cache부터 보존합니다. 순환 위치는 제거하지 않아 누락된 cache가 조회를 굶기지 않습니다.
// JSONB가 구분자 뒤에 추가하는 공백도 보수적으로 계산해 DB의 cursor::text 16KiB 제약을 넘지 않게 합니다.
func marshalPublicationCursor(cursor publicationCursor) (jsontext.Value, error) {
	for {
		raw, err := jsonv2.Marshal(cursor)
		if err != nil {
			return nil, fmt.Errorf("marshal publication cursor: %w", err)
		}

		storedSize := len(raw)
		for _, value := range raw {
			if value == ':' || value == ',' {
				storedSize++
			}
		}

		if storedSize <= maxPublicationCursorBytes {
			return raw, nil
		}

		if len(cursor.Entries) == 0 {
			return nil, collecterr.New(collecterr.ResponseTooLarge, collecterr.ClassResourceLimit, "publication cursor exceeds storage limit")
		}

		cursor.Entries = cursor.Entries[:len(cursor.Entries)-1]
	}
}
