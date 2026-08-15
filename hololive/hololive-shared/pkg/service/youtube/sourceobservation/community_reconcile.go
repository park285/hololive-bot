package sourceobservation

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/community"
)

func (c *Consumer) reconcileCommunity(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (community.Batch, ReconcileResult, bool, error) {
	payload, err := decodeCommunityPayload(claimed)
	if err != nil {
		return community.Batch{}, ReconcileResult{}, false, err
	}
	if err := lockCommunitySubject(ctx, tx, claimed.Provider, claimed.ObservationKind, payload.ChannelID); err != nil {
		return community.Batch{}, ReconcileResult{}, false, err
	}
	head, err := loadCommunitySubjectHead(ctx, tx, claimed.Provider, claimed.ObservationKind, payload.ChannelID)
	if err != nil {
		return community.Batch{}, ReconcileResult{}, false, err
	}
	if head.supersedes(claimed) {
		return community.Batch{}, ReconcileResult{Applications: []Application{{
			EntityKind: "community_subject_head",
			EntityKey:  payload.ChannelID,
			Decision:   "STALE_SKIPPED",
		}}}, false, nil
	}
	watermark, initialized, err := loadCommunityWatermark(ctx, tx, payload.ChannelID)
	if err != nil {
		return community.Batch{}, ReconcileResult{}, false, err
	}
	persisted := community.ArtifactsFromPayload(
		&payload,
		initialized,
		watermark,
		claimed.EffectiveAt,
		c.keywords,
	)
	if err := c.writer.PersistTx(ctx, tx, &persisted); err != nil {
		return community.Batch{}, ReconcileResult{}, false, err
	}
	if err := saveCommunitySubjectHead(ctx, tx, claimed); err != nil {
		return community.Batch{}, ReconcileResult{}, false, err
	}
	return persisted, ReconcileResult{Applications: communityApplications(payload.ChannelID, &persisted)}, true, nil
}

func communityApplications(channelID string, persisted *community.Batch) []Application {
	applications := make([]Application, 0, len(persisted.Posts)+1)
	applications = append(applications, Application{
		EntityKind: "community_subject_head",
		EntityKey:  channelID,
		Decision:   "APPLIED",
	})
	for i := range persisted.Posts {
		applications = append(applications, Application{
			EntityKind: "community_post",
			EntityKey:  persisted.Posts[i].PostID,
			Decision:   "APPLIED",
		})
	}
	return applications
}
