package sourceobservation

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (p *ChannelProfileV1) normalizeAndValidate(subject string) error {
	if p.ChannelID != subject {
		return fmt.Errorf("channel profile channel does not match subject")
	}
	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return err
	}
	if err := validateProfileFieldValues(p); err != nil {
		return err
	}
	return validateCoveredProfileFields(p)
}

func validateProfileFieldValues(p *ChannelProfileV1) error {
	for name, field := range profileFieldsByDisplayName(p) {
		if err := validateProfileFieldValue(name, field); err != nil {
			return err
		}
	}
	return nil
}

func profileFieldsByDisplayName(p *ChannelProfileV1) map[string]FieldValue[string] {
	return map[string]FieldValue[string]{
		"handle": p.Handle, "description": p.Description, "country": p.Country, "joined date": p.JoinedDate,
	}
}

func validateProfileFieldValue(name string, field FieldValue[string]) error {
	limit := 4096
	if name == "handle" || name == "country" || name == "joined date" {
		limit = 256
	}
	if !field.Present && field.Value != "" {
		return fmt.Errorf("absent profile field %s contains a value", name)
	}
	if len(field.Value) > limit {
		return fmt.Errorf("profile field %s exceeds %d bytes", name, limit)
	}
	return nil
}

func validateCoveredProfileFields(p *ChannelProfileV1) error {
	coveredFields := stringSet(p.Coverage.Fields)
	for name, field := range map[string]FieldValue[string]{
		"handle": p.Handle, "description": p.Description, "country": p.Country, "joined_date": p.JoinedDate,
	} {
		if err := requireCoveredField(coveredFields, name, field.Present); err != nil {
			return fmt.Errorf("profile field %q is outside coverage", name)
		}
	}
	return nil
}

func (p *ChannelPhotoV1) normalizeAndValidate(subject string) error {
	if p.ChannelID != subject {
		return fmt.Errorf("channel photo channel does not match subject")
	}
	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return err
	}
	if err := preparePhotoVariants(p); err != nil {
		return err
	}
	coveredVariants := stringSet(p.Coverage.Variants)
	for i := range p.Variants {
		if err := validatePhotoVariant(&p.Variants[i], coveredVariants); err != nil {
			return err
		}
	}
	sort.Slice(p.Variants, lessPhotoVariantIndex(p.Variants))
	return nil
}

func preparePhotoVariants(p *ChannelPhotoV1) error {
	if len(p.Variants) > 20 {
		return fmt.Errorf("photo variant count exceeds 20")
	}
	if p.Variants == nil {
		p.Variants = []PhotoVariantV1{}
	}
	return nil
}

func validatePhotoVariant(variant *PhotoVariantV1, coveredVariants map[string]struct{}) error {
	if variant.Kind != "avatar" && variant.Kind != "banner" {
		return fmt.Errorf("unsupported photo variant kind %q", variant.Kind)
	}
	if _, ok := coveredVariants[variant.Kind]; !ok {
		return fmt.Errorf("photo variant %q is outside coverage", variant.Kind)
	}
	if err := validateHTTPSURL("photo URL", variant.URL); err != nil {
		return err
	}
	if err := validatePhotoVariantDetails(variant); err != nil {
		return err
	}
	return nil
}

func validatePhotoVariantDetails(variant *PhotoVariantV1) error {
	if variant.Width < 0 || variant.Width > 20000 || variant.Height < 0 || variant.Height > 20000 {
		return fmt.Errorf("photo dimensions are outside the accepted range")
	}
	if err := validateOptionalText("stable media id", variant.StableMediaID, 512); err != nil {
		return err
	}
	if invalidPhotoFingerprint(variant.ContentFingerprint) {
		return fmt.Errorf("photo content fingerprint must be a lowercase sha256")
	}
	return nil
}

func invalidPhotoFingerprint(value string) bool {
	return value != "" && (len(value) != 64 || !lowercaseSHA256Pattern.MatchString(value))
}

func lessPhotoVariantIndex(variants []PhotoVariantV1) func(int, int) bool {
	return func(i, j int) bool {
		return photoVariantLess(&variants[i], &variants[j])
	}
}

func photoVariantLess(left, right *PhotoVariantV1) bool {
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.StableMediaID != right.StableMediaID {
		return left.StableMediaID < right.StableMediaID
	}
	return photoVariantLessRemainder(left, right)
}

func photoVariantLessRemainder(left, right *PhotoVariantV1) bool {
	if left.ContentFingerprint != right.ContentFingerprint {
		return left.ContentFingerprint < right.ContentFingerprint
	}
	if left.URL != right.URL {
		return left.URL < right.URL
	}
	if left.Width != right.Width {
		return left.Width < right.Width
	}
	return left.Height < right.Height
}

func (p *ScheduleSnapshotV1) normalizeAndValidate(subject string) error {
	if p.GroupKey != subject {
		return fmt.Errorf("schedule group does not match subject")
	}
	if err := p.Coverage.normalizeAndValidate(subject); err != nil {
		return err
	}
	if err := prepareScheduleItems(p); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(p.Items))
	for i := range p.Items {
		if err := normalizeScheduleItem(&p.Items[i], p.Coverage, seen); err != nil {
			return err
		}
	}
	sort.Slice(p.Items, func(i, j int) bool { return p.Items[i].ExternalID < p.Items[j].ExternalID })
	return nil
}

func prepareScheduleItems(p *ScheduleSnapshotV1) error {
	if len(p.Items) > 2000 {
		return fmt.Errorf("schedule item count exceeds 2000")
	}
	if p.Items == nil {
		p.Items = []ScheduleItemV1{}
	}
	return nil
}

func normalizeScheduleItem(item *ScheduleItemV1, coverage ScheduleCoverageV1, seen map[string]struct{}) error {
	if err := validateScheduleItemIdentity(item); err != nil {
		return err
	}
	if err := validateScheduleItemTiming(item, coverage); err != nil {
		return err
	}
	if err := normalizeCollaboTalentNames(item); err != nil {
		return err
	}
	if _, ok := seen[item.ExternalID]; ok {
		return fmt.Errorf("duplicate schedule external id %q", item.ExternalID)
	}
	seen[item.ExternalID] = struct{}{}
	return nil
}

func normalizeCollaboTalentNames(item *ScheduleItemV1) error {
	names := make([]string, 0, len(item.CollaboTalentNames))
	for _, name := range item.CollaboTalentNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > MaxScheduleCollaboTalentNameBytes {
			return fmt.Errorf("schedule collabo talent name exceeds %d bytes", MaxScheduleCollaboTalentNameBytes)
		}
		names = append(names, trimmed)
		if len(names) > MaxScheduleCollaboTalentNames {
			return fmt.Errorf("schedule collabo talent names exceed %d", MaxScheduleCollaboTalentNames)
		}
	}
	item.CollaboTalentNames = names
	return nil
}

func validateScheduleItemIdentity(item *ScheduleItemV1) error {
	if err := validateIdentifier("schedule external id", item.ExternalID, 256); err != nil {
		return err
	}
	if err := validateOptionalIdentifier("schedule video id", item.VideoID, 128); err != nil {
		return err
	}
	return validateOptionalIdentifier("schedule channel id", item.ChannelID, 256)
}

func validateOptionalIdentifier(name, value string, maxLength int) error {
	if value == "" {
		return nil
	}
	return validateIdentifier(name, value, maxLength)
}

func validateScheduleItemTiming(item *ScheduleItemV1, coverage ScheduleCoverageV1) error {
	if strings.TrimSpace(item.Title) == "" || len(item.Title) > 4096 || item.ScheduledAt.IsZero() {
		return fmt.Errorf("schedule item title or scheduled time is invalid")
	}
	item.ScheduledAt = item.ScheduledAt.UTC()
	if scheduleItemOutsideWindow(item.ScheduledAt, coverage) {
		return fmt.Errorf("schedule item time is outside coverage")
	}
	if err := normalizeOptionalTime(&item.EndedAt); err != nil {
		return fmt.Errorf("schedule ended at: %w", err)
	}
	return nil
}

func scheduleItemOutsideWindow(scheduledAt time.Time, coverage ScheduleCoverageV1) bool {
	return coverage.WindowStart != nil && scheduledAt.Before(*coverage.WindowStart) ||
		coverage.WindowEnd != nil && scheduledAt.After(*coverage.WindowEnd)
}

func MarshalPayloadV1(value any) (json.RawMessage, error) {
	if err := validateCanonicalJSONStrings(value); err != nil {
		return nil, fmt.Errorf("marshal source observation payload: %w", err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal source observation payload: %w", err)
	}
	canonical, err := CanonicalizeJSON(raw)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}
