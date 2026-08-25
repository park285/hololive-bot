package sourceobservation

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/community"
)

const (
	communityWindowEntityKind = "community_window"
	communityWindowDecision   = "CANONICALIZED"
)

func (c *Consumer) reconcileCommunity(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (community.Batch, ReconcileResult, bool, error) {
	payload, err := decodeCommunityPayload(claimed)
	if err != nil {
		return community.Batch{}, ReconcileResult{}, false, fmt.Errorf("decode community payload: %w", err)
	}

	if lockErr := lockCommunitySubject(ctx, tx, claimed.Provider, claimed.ObservationKind, payload.ChannelID); lockErr != nil {
		return community.Batch{}, ReconcileResult{}, false, fmt.Errorf("lock community subject: %w", lockErr)
	}

	head, err := loadCommunitySubjectHead(ctx, tx, claimed.Provider, claimed.ObservationKind, payload.ChannelID)
	if err != nil {
		return community.Batch{}, ReconcileResult{}, false, fmt.Errorf("load community subject head: %w", err)
	}

	if head.supersedes(claimed) {
		return community.Batch{}, ReconcileResult{Applications: []Application{{
			EntityKind: "community_subject_head",
			EntityKey:  payload.ChannelID,
			Decision:   "STALE_SKIPPED",
		}}}, false, nil
	}

	watermark, err := loadCommunityWatermark(ctx, tx, payload.ChannelID)
	if err != nil {
		return community.Batch{}, ReconcileResult{}, false, fmt.Errorf("load community watermark: %w", err)
	}

	notifyUnseen, knownPostIDs, err := communityNotificationState(
		ctx,
		tx,
		head.observationID,
		payload.ChannelID,
		watermark.Initialized,
		community.CanonicalPostIDs(payload.Posts),
	)
	if err != nil {
		return community.Batch{}, ReconcileResult{}, false, fmt.Errorf("load community notification state: %w", err)
	}

	persisted := community.ArtifactsFromPayload(
		&payload,
		notifyUnseen,
		knownPostIDs,
		claimed.EffectiveAt,
		c.keywords,
	)
	if err := c.writer.PersistTx(ctx, tx, &persisted); err != nil {
		return community.Batch{}, ReconcileResult{}, false, fmt.Errorf("persist tx: %w", err)
	}

	if err := saveCommunitySubjectHead(ctx, tx, claimed); err != nil {
		return community.Batch{}, ReconcileResult{}, false, fmt.Errorf("save community subject head: %w", err)
	}

	return persisted, ReconcileResult{Applications: communityApplications(payload.ChannelID, &persisted)}, true, nil
}

func communityNotificationState(
	ctx context.Context,
	tx dbx.Tx,
	observationID int64,
	channelID string,
	initialized bool,
	postIDs []string,
) (bool, map[string]struct{}, error) {
	windowReady, err := loadCommunityWindowReady(ctx, tx, observationID, channelID)
	if err != nil {
		return false, nil, fmt.Errorf("load community window state: %w", err)
	}

	if !initialized || !windowReady {
		return false, map[string]struct{}{}, nil
	}

	knownPostIDs, err := loadKnownCommunityPostIDs(ctx, tx, channelID, postIDs)
	if err != nil {
		return false, nil, fmt.Errorf("load known community posts: %w", err)
	}

	return true, knownPostIDs, nil
}

func communityApplications(channelID string, persisted *community.Batch) []Application {
	applications := make([]Application, 0, len(persisted.Posts)+2)

	applications = append(
		applications,
		Application{EntityKind: "community_subject_head", EntityKey: channelID, Decision: "APPLIED"},
		Application{EntityKind: communityWindowEntityKind, EntityKey: channelID, Decision: communityWindowDecision},
	)

	for i := range persisted.Posts {
		applications = append(applications, Application{
			EntityKind: "community_post",
			EntityKey:  persisted.Posts[i].PostID,
			Decision:   "APPLIED",
		})
	}

	return applications
}
