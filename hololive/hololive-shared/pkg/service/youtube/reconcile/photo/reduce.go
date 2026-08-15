package photo

import (
	"fmt"
	"net/url"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func Reduce(state State, evidence Evidence, policy Policy) (Decision, error) { //nolint:gocritic // public pure reducer copies inputs before private mutation
	if evidence.Sample.ChannelID == "" {
		return Decision{}, fmt.Errorf("channel photo reducer received empty channel id")
	}
	workingState := state.clone()
	workingEvidence := evidence.clone()
	sample := &workingEvidence.Sample
	sample.Provider = workingEvidence.Provider
	sample.ObservationID = workingEvidence.ObservationID
	head := workingState.Head
	head.ChannelID = sample.ChannelID
	if head.Kinds == nil {
		head.Kinds = map[string]Canonical{}
	}
	apps := make([]Application, 0, 4)
	conflicts := make([]Conflict, 0, 2)
	var product map[string]Canonical
	byKind, err := groupVariants(sample.Variants)
	if err != nil {
		return Decision{}, err
	}
	product, apps, conflicts, err = reducePhotoKinds(&head, byKind, sample, policy, apps, conflicts)
	if err != nil {
		return Decision{}, err
	}
	if len(apps) == 0 {
		apps = append(apps, Application{
			EntityKind: "youtube_channel_photo", EntityKey: sample.ChannelID, Decision: "RAW_RETAINED",
		})
	}
	return Decision{
		Sample:       sample,
		Head:         head,
		WriteProduct: product,
		Conflicts:    conflicts,
		Applications: apps,
	}, nil
}

func reducePhotoKinds(
	head *Head,
	byKind map[string][]Variant,
	sample *Sample,
	policy Policy,
	apps []Application,
	conflicts []Conflict,
) (map[string]Canonical, []Application, []Conflict, error) {
	if missingPhotoState(head, sample) {
		return nil, nil, nil, fmt.Errorf("reduce channel photo kinds: nil state")
	}
	product := map[string]Canonical{}
	for _, kind := range []string{"avatar", "banner"} {
		variants := byKind[kind]
		if len(variants) == 0 {
			continue
		}
		current := head.Kinds[kind]
		next, writeProduct, kindApps, kindConflicts, err := reduceKind(&current, kind, variants, sample, policy)
		if err != nil {
			return nil, nil, nil, err
		}
		head.Kinds[kind] = next
		apps = append(apps, kindApps...)
		conflicts = append(conflicts, kindConflicts...)
		if writeProduct {
			product[kind] = next
		}
	}
	return product, apps, conflicts, nil
}

func missingPhotoState(head *Head, sample *Sample) bool {
	return head == nil || sample == nil
}

func reduceKind(current *Canonical, kind string, variants []Variant, sample *Sample, policy Policy) (Canonical, bool, []Application, []Conflict, error) {
	if current == nil || sample == nil {
		return Canonical{}, false, nil, nil, fmt.Errorf("reduce channel photo kind: nil state")
	}
	key := sample.ChannelID + "/" + kind
	identified := identifiedVariants(variants)
	if len(identified) == 0 {
		return *current, false, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "RAW_RETAINED"}}, nil, nil
	}
	chosen, ok, conflict := chooseIdentity(identified)
	if !ok {
		return *current, false, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CONFLICT"}}, []Conflict{conflict}, nil
	}
	rejected, next, apps, conflicts, err := rejectPhotoChange(current, kind, key, Identity(&chosen), sample)
	if err != nil {
		return Canonical{}, false, nil, nil, err
	}
	if rejected {
		return next, false, apps, conflicts, nil
	}
	return applyPhotoChange(current, key, &chosen, sample, policy)
}

func rejectPhotoChange(current *Canonical, kind, key, identity string, sample *Sample) (bool, Canonical, []Application, []Conflict, error) {
	if current == nil || sample == nil {
		return false, Canonical{}, nil, nil, fmt.Errorf("reject channel photo change: nil state")
	}
	if olderPhotoSample(current, sample) {
		return true, *current, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "OLDER_RETAINED"}}, nil, nil
	}
	if conflictingPhotoSample(current, identity, sample) {
		return true, *current, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CONFLICT"}}, []Conflict{{
			FieldName:            kind,
			ExistingValueSHA256:  contract.SHA256Hex([]byte(current.Identity)),
			AttemptedValueSHA256: contract.SHA256Hex([]byte(identity)),
		}}, nil
	}
	if current.Identity == identity {
		if err := resetCandidate(current); err != nil {
			return false, Canonical{}, nil, nil, err
		}
		return true, *current, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CANONICAL_UNCHANGED"}}, nil, nil
	}
	return false, *current, nil, nil, nil
}

func olderPhotoSample(current *Canonical, sample *Sample) bool {
	return current.Identity != "" && current.EffectiveAt != nil && sample.EffectiveAt.Before(*current.EffectiveAt)
}

func conflictingPhotoSample(current *Canonical, identity string, sample *Sample) bool {
	return current.Identity != "" && current.EffectiveAt != nil &&
		sample.EffectiveAt.Equal(*current.EffectiveAt) && current.Identity != identity
}

func applyPhotoChange(current *Canonical, key string, chosen *Variant, sample *Sample, policy Policy) (Canonical, bool, []Application, []Conflict, error) {
	if missingPhotoChangeState(current, chosen, sample) {
		return Canonical{}, false, nil, nil, fmt.Errorf("apply channel photo change: nil state")
	}
	identity := Identity(chosen)
	if !policy.ChangeEnabled() {
		return *current, false, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CHANGE_DISABLED"}}, nil, nil
	}
	if !sample.Complete {
		return *current, false, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CHANGE_PENDING"}}, nil, nil
	}
	if replayedPhotoChange(current, identity, sample) {
		return *current, false, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "REPLAY"}}, nil, nil
	}
	if err := trackPhotoCandidate(current, chosen, identity, sample); err != nil {
		return Canonical{}, false, nil, nil, err
	}
	if pendingPhotoChange(current, sample, policy) {
		return *current, false, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CHANGE_PENDING"}}, nil, nil
	}
	current.Identity = identity
	current.URL = chosen.URL
	current.Width = chosen.Width
	current.Height = chosen.Height
	current.EffectiveAt = copyTime(sample.EffectiveAt)
	if err := resetCandidate(current); err != nil {
		return Canonical{}, false, nil, nil, err
	}
	return *current, true, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "APPLIED"}}, nil, nil
}

func missingPhotoChangeState(current *Canonical, chosen *Variant, sample *Sample) bool {
	return current == nil || chosen == nil || sample == nil
}

func replayedPhotoChange(current *Canonical, identity string, sample *Sample) bool {
	return current.LastAt != nil && current.LastAt.Equal(sample.ScheduledFor) && current.Candidate == identity
}

func pendingPhotoChange(current *Canonical, sample *Sample, policy Policy) bool {
	return current.Slots < policy.ChangeMinObservations || current.FirstRx == nil ||
		sample.ReceivedAt.Before(current.FirstRx.Add(policy.ChangeStability))
}

func trackPhotoCandidate(current *Canonical, chosen *Variant, identity string, sample *Sample) error {
	if current == nil || chosen == nil || sample == nil {
		return fmt.Errorf("track channel photo candidate: nil state")
	}
	if current.Candidate != identity {
		current.Candidate = identity
		current.CandidateURL = chosen.URL
		current.CandidateW = chosen.Width
		current.CandidateH = chosen.Height
		current.Slots = 1
		current.FirstAt = copyTime(sample.ScheduledFor)
		current.LastAt = copyTime(sample.ScheduledFor)
		current.FirstRx = copyTime(sample.ReceivedAt)
		return nil
	}
	current.Slots++
	current.LastAt = copyTime(sample.ScheduledFor)
	current.CandidateURL = chosen.URL
	current.CandidateW = chosen.Width
	current.CandidateH = chosen.Height
	return nil
}

func groupVariants(variants []Variant) (map[string][]Variant, error) {
	grouped := map[string][]Variant{}
	for i := range variants {
		variant := variants[i]
		if variant.Kind != "avatar" && variant.Kind != "banner" {
			return nil, fmt.Errorf("unsupported photo variant kind %q", variant.Kind)
		}
		if err := validateStoredURL(variant.URL); err != nil {
			return nil, err
		}
		grouped[variant.Kind] = append(grouped[variant.Kind], variant)
	}
	return grouped, nil
}

func validateStoredURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("photo URL must be an absolute HTTPS URL")
	}
	return nil
}

func identifiedVariants(variants []Variant) []Variant {
	identified := make([]Variant, 0, len(variants))
	for i := range variants {
		if Identity(&variants[i]) == "" {
			continue
		}
		identified = append(identified, variants[i])
	}
	return identified
}

func chooseIdentity(variants []Variant) (Variant, bool, Conflict) {
	if len(variants) == 0 {
		return Variant{}, false, Conflict{}
	}
	chosen := variants[0]
	for i := 1; i < len(variants); i++ {
		if Identity(&variants[i]) != Identity(&chosen) {
			return Variant{}, false, Conflict{
				FieldName:            chosen.Kind,
				ExistingValueSHA256:  contract.SHA256Hex([]byte(Identity(&chosen))),
				AttemptedValueSHA256: contract.SHA256Hex([]byte(Identity(&variants[i]))),
			}
		}
		if variants[i].Width*variants[i].Height > chosen.Width*chosen.Height {
			chosen = variants[i]
		}
	}
	return chosen, true, Conflict{}
}

func resetCandidate(current *Canonical) error {
	if current == nil {
		return fmt.Errorf("reset channel photo candidate: nil state")
	}
	current.Candidate = ""
	current.CandidateURL = ""
	current.CandidateW = 0
	current.CandidateH = 0
	current.Slots = 0
	current.FirstAt = nil
	current.LastAt = nil
	current.FirstRx = nil
	return nil
}

func copyTime(value time.Time) *time.Time {
	copied := value.UTC()
	return &copied
}
