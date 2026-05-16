package observation

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

type observationComparisonMetadata struct {
	titleHint         string
	actualPublishedAt *time.Time
}

type observationCommunityPostMetadataRow struct {
	PostID      string     `gorm:"column:post_id"`
	ContentText string     `gorm:"column:content_text"`
	PublishedAt *time.Time `gorm:"column:published_at"`
}

type observationShortMetadataRow struct {
	VideoID     string     `gorm:"column:video_id"`
	Title       string     `gorm:"column:title"`
	PublishedAt *time.Time `gorm:"column:published_at"`
}

func (r *GormRepository) EnrichObservationPostComparisonInputs(
	ctx context.Context,
	inputs []ObservationPostComparisonInput,
) ([]ObservationPostComparisonInput, error) {
	if len(inputs) == 0 {
		return []ObservationPostComparisonInput{}, nil
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("enrich observation post comparison inputs: db is nil")
	}

	metadataByKey, err := r.loadObservationComparisonMetadata(ctx, inputs)
	if err != nil {
		return nil, fmt.Errorf("enrich observation post comparison inputs: %w", err)
	}

	enriched := make([]ObservationPostComparisonInput, 0, len(inputs))
	for i := range inputs {
		normalized := normalizeObservationPostComparisonComparableInput(inputs[i])
		enriched = append(enriched, enrichObservationPostComparisonInput(normalized, metadataByKey))
	}

	return enriched, nil
}

func enrichObservationPostComparisonInput(
	input ObservationPostComparisonInput,
	metadataByKey map[string]observationComparisonMetadata,
) ObservationPostComparisonInput {
	metadata, ok := metadataByKey[observationComparisonMetadataKey(input.Kind, input.CanonicalPostID, input.ContentID)]
	if !ok {
		return input
	}
	if strings.TrimSpace(input.TitleHint) == "" {
		input.TitleHint = metadata.titleHint
	}
	if input.ActualPublishedAt == nil && metadata.actualPublishedAt != nil {
		input.ActualPublishedAt = cloneObservationComparisonTime(metadata.actualPublishedAt)
	}
	return input
}

func (r *GormRepository) loadObservationComparisonMetadata(
	ctx context.Context,
	inputs []ObservationPostComparisonInput,
) (map[string]observationComparisonMetadata, error) {
	communityCanonicalIDs, shortCanonicalIDs := collectObservationComparisonCanonicalIDs(inputs)
	metadataByKey := make(map[string]observationComparisonMetadata, len(communityCanonicalIDs)+len(shortCanonicalIDs))

	communityMetadata, err := r.loadObservationCommunityPostMetadata(ctx, communityCanonicalIDs)
	if err != nil {
		return nil, err
	}
	maps.Copy(metadataByKey, communityMetadata)

	shortMetadata, err := r.loadObservationShortMetadata(ctx, shortCanonicalIDs)
	if err != nil {
		return nil, err
	}
	maps.Copy(metadataByKey, shortMetadata)

	return metadataByKey, nil
}

func collectObservationComparisonCanonicalIDs(inputs []ObservationPostComparisonInput) ([]string, []string) {
	communitySeen := make(map[string]struct{}, len(inputs))
	shortSeen := make(map[string]struct{}, len(inputs))
	communityCanonicalIDs := make([]string, 0, len(inputs))
	shortCanonicalIDs := make([]string, 0, len(inputs))

	for i := range inputs {
		canonicalID := normalizeObservationComparisonCanonicalPostID(inputs[i].Kind, inputs[i].CanonicalPostID, inputs[i].ContentID)
		if canonicalID == "" {
			continue
		}

		communityCanonicalIDs, shortCanonicalIDs = appendObservationComparisonCanonicalID(
			inputs[i].Kind,
			canonicalID,
			communitySeen,
			shortSeen,
			communityCanonicalIDs,
			shortCanonicalIDs,
		)
	}

	return communityCanonicalIDs, shortCanonicalIDs
}

func appendObservationComparisonCanonicalID(
	kind domain.OutboxKind,
	canonicalID string,
	communitySeen map[string]struct{},
	shortSeen map[string]struct{},
	communityCanonicalIDs []string,
	shortCanonicalIDs []string,
) ([]string, []string) {
	switch kind {
	case domain.OutboxKindCommunityPost:
		return appendUniqueObservationComparisonID(communitySeen, communityCanonicalIDs, canonicalID), shortCanonicalIDs
	case domain.OutboxKindNewShort:
		return communityCanonicalIDs, appendUniqueObservationComparisonID(shortSeen, shortCanonicalIDs, canonicalID)
	default:
		return communityCanonicalIDs, shortCanonicalIDs
	}
}

func appendUniqueObservationComparisonID(seen map[string]struct{}, ids []string, canonicalID string) []string {
	if _, ok := seen[canonicalID]; ok {
		return ids
	}
	seen[canonicalID] = struct{}{}
	return append(ids, canonicalID)
}

func (r *GormRepository) loadObservationCommunityPostMetadata(
	ctx context.Context,
	canonicalIDs []string,
) (map[string]observationComparisonMetadata, error) {
	rawIDs := observationComparisonRawIDs(domain.OutboxKindCommunityPost, canonicalIDs)
	metadataByKey := make(map[string]observationComparisonMetadata, len(rawIDs))
	if len(rawIDs) == 0 {
		return metadataByKey, nil
	}

	var rows []observationCommunityPostMetadataRow
	if err := r.db.WithContext(ctx).
		Table((domain.YouTubeCommunityPost{}).TableName()).
		Select("post_id, content_text, published_at").
		Where("post_id IN ?", rawIDs).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load observation community metadata: query rows: %w", err)
	}

	for i := range rows {
		canonicalID, err := ytcontentid.ForCommunity(rows[i].PostID)
		if err != nil {
			continue
		}
		metadataByKey[observationComparisonMetadataKey(domain.OutboxKindCommunityPost, canonicalID, "")] = observationComparisonMetadata{
			titleHint:         observationComparisonNormalizeTitleHint(rows[i].ContentText),
			actualPublishedAt: cloneObservationComparisonTime(rows[i].PublishedAt),
		}
	}

	return metadataByKey, nil
}

func (r *GormRepository) loadObservationShortMetadata(
	ctx context.Context,
	canonicalIDs []string,
) (map[string]observationComparisonMetadata, error) {
	rawIDs := observationComparisonRawIDs(domain.OutboxKindNewShort, canonicalIDs)
	metadataByKey := make(map[string]observationComparisonMetadata, len(rawIDs))
	if len(rawIDs) == 0 {
		return metadataByKey, nil
	}

	var rows []observationShortMetadataRow
	if err := r.db.WithContext(ctx).
		Table((domain.YouTubeVideo{}).TableName()).
		Select("video_id, title, published_at").
		Where("video_id IN ?", rawIDs).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load observation short metadata: query rows: %w", err)
	}

	for i := range rows {
		canonicalID, err := ytcontentid.ForShort(rows[i].VideoID)
		if err != nil {
			continue
		}
		metadataByKey[observationComparisonMetadataKey(domain.OutboxKindNewShort, canonicalID, "")] = observationComparisonMetadata{
			titleHint:         observationComparisonNormalizeTitleHint(rows[i].Title),
			actualPublishedAt: cloneObservationComparisonTime(rows[i].PublishedAt),
		}
	}

	return metadataByKey, nil
}

func observationComparisonMetadataKey(kind domain.OutboxKind, canonicalPostID string, contentID string) string {
	resolvedCanonicalID := normalizeObservationComparisonCanonicalPostID(kind, canonicalPostID, contentID)
	if resolvedCanonicalID == "" {
		resolvedCanonicalID = strings.TrimSpace(canonicalPostID)
	}
	if resolvedCanonicalID == "" {
		resolvedCanonicalID = strings.TrimSpace(contentID)
	}
	return string(kind) + "\x00" + resolvedCanonicalID
}

func observationComparisonRawIDs(kind domain.OutboxKind, canonicalIDs []string) []string {
	seen := make(map[string]struct{}, len(canonicalIDs))
	rawIDs := make([]string, 0, len(canonicalIDs))
	for i := range canonicalIDs {
		rawID, ok := observationComparisonRawID(kind, canonicalIDs[i])
		if !ok {
			continue
		}
		if _, exists := seen[rawID]; exists {
			continue
		}
		seen[rawID] = struct{}{}
		rawIDs = append(rawIDs, rawID)
	}
	return rawIDs
}

func observationComparisonRawID(kind domain.OutboxKind, candidate string) (string, bool) {
	switch kind {
	case domain.OutboxKindCommunityPost:
		rawID, err := ytcontentid.NormalizeCommunityPostID(candidate)
		return strings.TrimSpace(rawID), err == nil && strings.TrimSpace(rawID) != ""
	case domain.OutboxKindNewShort:
		rawID, err := ytcontentid.NormalizeShortVideoID(candidate)
		return strings.TrimSpace(rawID), err == nil && strings.TrimSpace(rawID) != ""
	default:
		return "", false
	}
}

func observationComparisonNormalizeTitleHint(value string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if normalized == "" {
		return ""
	}
	runes := []rune(normalized)
	if len(runes) > 120 {
		return strings.TrimSpace(string(runes[:120]))
	}
	return normalized
}
