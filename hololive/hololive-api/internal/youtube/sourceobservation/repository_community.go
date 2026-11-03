package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type communitySubjectHead struct {
	observationID int64
	effectiveAt   time.Time
}

func (h communitySubjectHead) supersedes(observation *Observation) bool {
	return h.effectiveAt.After(observation.EffectiveAt) ||
		h.effectiveAt.Equal(observation.EffectiveAt) && h.observationID >= observation.ID
}

func lockCommunitySubject(
	ctx context.Context,
	tx dbx.Tx,
	provider contract.Provider,
	kind contract.ObservationKind,
	subjectKey string,
) error {
	if _, err := tx.Exec(
		ctx,
		lockCommunitySubjectSQL,
		provider,
		kind,
		subjectKey,
	); err != nil {
		return fmt.Errorf("lock community subject: %w", err)
	}

	return nil
}

func loadCommunitySubjectHead(
	ctx context.Context,
	q dbx.Querier,
	provider contract.Provider,
	kind contract.ObservationKind,
	subjectKey string,
) (communitySubjectHead, error) {
	var head communitySubjectHead

	err := q.QueryRow(
		ctx,
		loadCommunitySubjectHeadSQL,
		provider,
		kind,
		subjectKey,
	).Scan(&head.observationID, &head.effectiveAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return communitySubjectHead{}, nil
	}

	if err != nil {
		return communitySubjectHead{}, fmt.Errorf("load community subject head: %w", err)
	}

	return head, nil
}

func saveCommunitySubjectHead(
	ctx context.Context,
	tx dbx.Tx,
	observation *Observation,
) error {
	if _, err := tx.Exec(
		ctx,
		saveCommunitySubjectHeadSQL,
		observation.Provider,
		observation.ObservationKind,
		observation.SubjectKey,
		observation.ID,
		observation.EvidenceSHA256,
		observation.EffectiveAt,
	); err != nil {
		return fmt.Errorf("save community subject head: %w", err)
	}

	return nil
}

func loadCommunityWatermark(
	ctx context.Context,
	q dbx.Querier,
	channelID string,
) (domain.YouTubeContentWatermark, error) {
	watermark, err := loadTypedWatermark(ctx, q, channelID, domain.WatermarkTypeCommunityPost)
	if err != nil {
		return domain.YouTubeContentWatermark{}, fmt.Errorf("load typed watermark: %w", err)
	}

	return watermark, nil
}

func loadCommunityWindowReady(
	ctx context.Context,
	q dbx.Querier,
	observationID int64,
	channelID string,
) (bool, error) {
	if observationID <= 0 {
		return false, nil
	}

	var ready bool

	if err := q.QueryRow(
		ctx,
		loadCommunityWindowReadySQL,
		observationID,
		communityWindowEntityKind,
		channelID,
		communityWindowDecision,
	).Scan(&ready); err != nil {
		return false, fmt.Errorf("query community window state: %w", err)
	}

	return ready, nil
}

func loadKnownCommunityPostIDs(
	ctx context.Context,
	q dbx.Querier,
	channelID string,
	postIDs []string,
) (map[string]struct{}, error) {
	known := make(map[string]struct{}, len(postIDs))
	if len(postIDs) == 0 {
		return known, nil
	}

	rows, err := q.Query(
		ctx,
		loadKnownCommunityPostIDsSQL,
		channelID,
		postIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("query known community posts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var postID string

		if err := rows.Scan(&postID); err != nil {
			return nil, fmt.Errorf("scan known community post: %w", err)
		}

		known[postID] = struct{}{}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query known community posts: %w", err)
	}

	return known, nil
}

func loadTypedWatermark(
	ctx context.Context,
	q dbx.Querier,
	channelID string,
	watermarkType domain.WatermarkType,
) (domain.YouTubeContentWatermark, error) {
	var (
		watermark     domain.YouTubeContentWatermark
		lastContentID *string
	)

	err := q.QueryRow(
		ctx,
		loadTypedWatermarkSQL,
		channelID,
		watermarkType,
	).Scan(
		&watermark.ChannelID,
		&watermark.WatermarkType,
		&watermark.Initialized,
		&lastContentID,
		&watermark.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.YouTubeContentWatermark{}, nil
	}

	if err != nil {
		return domain.YouTubeContentWatermark{}, fmt.Errorf("load community watermark: %w", err)
	}

	if lastContentID != nil {
		watermark.LastContentID = *lastContentID
	}

	return watermark, nil
}
