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

const ContentPublicationMaxCalls = 2

const (
	publicationCursorVersion    = 1
	maxPublicationCursorEntries = 48
	maxPublicationCursorBytes   = 16384
)

type ContentCursorReader interface {
	LatestCheckpointCursor(ctx context.Context, provider contract.Provider, kind contract.ObservationKind, subjectKey string) (jsontext.Value, error)
}

type publicationCursor struct {
	Version      int                      `json:"version"`
	ScheduledFor time.Time                `json:"scheduled_for"`
	HeadVideoID  string                   `json:"head_video_id"`
	NextPosition int                      `json:"next_position"`
	Entries      []publicationCursorEntry `json:"entries"`
}

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

	if !liveCheckRequestFailed(ctx, err) {
		return publicationCursorEntry{}, fmt.Errorf("fetch video publication: %w", err)
	}

	return publicationCursorEntry{VideoID: videoID, AttemptedAt: time.Now().UTC()}, nil
}

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

func classifyPublication(result *youtubejs.VideoLiveCheckResult, channelID string, checkedAt time.Time) contract.VideoPublicationV1 {
	unresolved := contract.VideoPublicationV1{Status: contract.VideoPublicationUnresolved, CheckedAt: checkedAt}

	if !result.IdentityConfirmed || result.ChannelID != channelID {
		return unresolved
	}

	if result.Availability == contract.VideoAvailabilityUnknown || result.Availability == contract.VideoAvailabilityPublicUnavailable {
		return unresolved
	}

	if result.IsUpcoming != nil && *result.IsUpcoming {
		if result.IsLiveContent == nil || *result.IsLiveContent || result.ScheduledAt == nil ||
			result.WaitingStateConfirmed == nil || !*result.WaitingStateConfirmed {
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

type storedPublicationCursor struct {
	scheduledFor time.Time
	entries      map[string]publicationCursorEntry
	headVideoID  string
	nextPosition int
}

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
		return storedPublicationCursor{}, collecterr.Wrap(contract.ErrorParserDrift, contract.ClassDataContract, fmt.Errorf("decode publication cursor: %w", decodeErr))
	}

	if !validPublicationCursorShape(len(raw), &cursor) {
		return storedPublicationCursor{}, collecterr.New(contract.ErrorParserDrift, contract.ClassDataContract, "publication cursor shape is invalid")
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

func validPublicationCursorShape(rawSize int, cursor *publicationCursor) bool {
	return rawSize <= maxPublicationCursorBytes && cursor.Version == publicationCursorVersion &&
		!cursor.ScheduledFor.IsZero() && cursor.NextPosition >= 0 &&
		len(cursor.HeadVideoID) <= 128 && len(cursor.Entries) <= maxPublicationCursorEntries
}

func publicationCursorEntries(stored []publicationCursorEntry, now time.Time) (map[string]publicationCursorEntry, error) {
	entries := make(map[string]publicationCursorEntry, len(stored))

	for i := range stored {
		entry := stored[i]
		if entry.VideoID == "" || len(entry.VideoID) > 128 || entry.AttemptedAt.IsZero() || entry.AttemptedAt.After(now) {
			return nil, collecterr.New(contract.ErrorParserDrift, contract.ClassDataContract, "publication cursor entry is invalid")
		}

		if _, duplicate := entries[entry.VideoID]; duplicate {
			return nil, collecterr.New(contract.ErrorParserDrift, contract.ClassDataContract, "publication cursor repeats a video")
		}

		if entry.Publication != nil {
			if err := entry.Publication.Validate(now); err != nil {
				return nil, collecterr.Wrap(contract.ErrorParserDrift, contract.ClassDataContract, fmt.Errorf("validate cached publication: %w", err))
			}
		}

		entries[entry.VideoID] = entry
	}

	return entries, nil
}

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
			return nil, collecterr.New(contract.ErrorResponseTooLarge, contract.ClassResourceLimit, "publication cursor exceeds storage limit")
		}

		cursor.Entries = cursor.Entries[:len(cursor.Entries)-1]
	}
}
