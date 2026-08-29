package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/content"
)

func (c *Consumer) reconcileContent(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (content.Decision, ReconcileResult, error) {
	evidence, err := evidenceFromObservation(claimed)
	if err != nil {
		return content.Decision{}, ReconcileResult{}, fmt.Errorf("evidence from observation: %w", err)
	}

	if lockErr := lockContentSubject(ctx, tx, claimed.ObservationKind, claimed.SubjectKey); lockErr != nil {
		return content.Decision{}, ReconcileResult{}, fmt.Errorf("lock content subject: %w", lockErr)
	}

	state, err := loadContentState(ctx, tx, claimed.ObservationKind, claimed.SubjectKey)
	if err != nil {
		return content.Decision{}, ReconcileResult{}, fmt.Errorf("load content state: %w", err)
	}

	decision, err := content.Reduce(state, evidence, c.grace)
	if err != nil {
		return content.Decision{}, ReconcileResult{}, fmt.Errorf("reduce: %w", err)
	}

	if persistErr := persistContentDecision(ctx, tx, c.writer, claimed, &decision); persistErr != nil {
		return content.Decision{}, ReconcileResult{}, fmt.Errorf("persist content decision: %w", persistErr)
	}

	premiereApplications, err := mergeContentPremieres(ctx, tx, claimed, &evidence)
	if err != nil {
		return content.Decision{}, ReconcileResult{}, fmt.Errorf("merge content Premieres: %w", err)
	}

	if err := saveCommunitySubjectHead(ctx, tx, claimed); err != nil {
		return content.Decision{}, ReconcileResult{}, fmt.Errorf("save community subject head: %w", err)
	}

	applications := mergeContentApplications(mapContentApplications(decision.Applications), premiereApplications)

	return decision, ReconcileResult{Applications: applications}, nil
}

func mergeContentApplications(contentApplications, premiereApplications []Application) []Application {
	premiereCount := min(len(premiereApplications), maxFinalizeApplicationCount)
	contentCount := min(len(contentApplications), maxFinalizeApplicationCount-premiereCount)
	applications := make([]Application, 0, contentCount+premiereCount)

	if contentCount > 0 {
		applications = append(applications, contentApplications[:contentCount]...)
	}

	if premiereCount > 0 {
		applications = append(applications, premiereApplications[:premiereCount]...)
	}

	return applications
}

func evidenceFromObservation(observation *Observation) (content.Evidence, error) {
	if observation.ObservationKind == contract.KindShortsList {
		out, err := shortsEvidence(observation)
		if err != nil {
			return out, fmt.Errorf("shorts evidence: %w", err)
		}

		return out, nil
	}

	out, err := videoEvidence(observation)
	if err != nil {
		return out, fmt.Errorf("video evidence: %w", err)
	}

	return out, nil
}

func videoEvidence(observation *Observation) (content.Evidence, error) {
	var payload contract.VideoListV1

	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
		return content.Evidence{}, fmt.Errorf("decode video list payload: %w", err)
	}

	return content.Evidence{
		Kind:           observation.ObservationKind,
		ObservationID:  observation.ID,
		ObservationKey: observation.ObservationKey,
		EvidenceSHA256: observation.EvidenceSHA256,
		ScopeSHA256:    observation.ScopeSHA256,
		ScheduledFor:   observation.ScheduledFor,
		EffectiveAt:    observation.EffectiveAt,
		ReceivedAt:     observation.ReceivedAt,
		Completeness:   observation.Completeness,
		Continuity:     observation.Continuity,
		Videos:         entitiesFromItems(payload.Videos, false),
		Coverage:       content.VideoCoverage(&payload.Coverage),
	}, nil
}

func shortsEvidence(observation *Observation) (content.Evidence, error) {
	var payload contract.ShortsListV1

	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
		return content.Evidence{}, fmt.Errorf("decode shorts list payload: %w", err)
	}

	return content.Evidence{
		Kind:           observation.ObservationKind,
		ObservationID:  observation.ID,
		ObservationKey: observation.ObservationKey,
		EvidenceSHA256: observation.EvidenceSHA256,
		ScopeSHA256:    observation.ScopeSHA256,
		ScheduledFor:   observation.ScheduledFor,
		EffectiveAt:    observation.EffectiveAt,
		ReceivedAt:     observation.ReceivedAt,
		Completeness:   observation.Completeness,
		Continuity:     observation.Continuity,
		Videos:         entitiesFromItems(payload.Videos, true),
		Coverage:       content.ShortsCoverage(&payload.Coverage),
	}, nil
}

func entitiesFromItems(items []contract.VideoListItemV1, shorts bool) []content.Entity {
	entities := make([]content.Entity, 0, len(items))
	for i := range items {
		entities = append(entities, content.Entity{
			VideoID:      items[i].VideoID,
			ChannelID:    items[i].ChannelID,
			Title:        items[i].Title,
			PublishedAt:  items[i].PublishedAt,
			ScheduledFor: items[i].ScheduledFor,
			IsPremiere:   items[i].IsPremiere,
			IsShort:      shorts,
		})
	}

	return entities
}

func mapContentApplications(items []content.Application) []Application {
	applications := make([]Application, len(items))
	for i := range items {
		applications[i] = Application{
			EntityKind: items[i].EntityKind,
			EntityKey:  items[i].EntityKey,
			Decision:   items[i].Decision,
		}
	}

	return applications
}
