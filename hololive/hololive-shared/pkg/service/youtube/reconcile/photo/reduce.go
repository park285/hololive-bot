package photo

import (
	"fmt"
	"net/url"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func Reduce(state State, evidence Evidence, policy Policy) (Decision, error) {
	if evidence.Sample.ChannelID == "" {
		return Decision{}, fmt.Errorf("channel photo reducer received empty channel id")
	}
	sample := evidence.Sample
	sample.Provider = evidence.Provider
	sample.ObservationID = evidence.ObservationID
	head := state.Head
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
	head, product, apps, conflicts = reducePhotoKinds(head, byKind, sample, policy, apps, conflicts)
	if len(apps) == 0 {
		apps = append(apps, Application{
			EntityKind: "youtube_channel_photo", EntityKey: sample.ChannelID, Decision: "RAW_RETAINED",
		})
	}
	return Decision{
		Sample:       &sample,
		Head:         head,
		WriteProduct: product,
		Conflicts:    conflicts,
		Applications: apps,
	}, nil
}

func reducePhotoKinds(
	head Head,
	byKind map[string][]Variant,
	sample Sample,
	policy Policy,
	apps []Application,
	conflicts []Conflict,
) (Head, map[string]Canonical, []Application, []Conflict) {
	product := map[string]Canonical{}
	for _, kind := range []string{"avatar", "banner"} {
		variants := byKind[kind]
		if len(variants) == 0 {
			continue
		}
		next, writeProduct, kindApps, kindConflicts := reduceKind(head.Kinds[kind], kind, variants, sample, policy)
		head.Kinds[kind] = next
		apps = append(apps, kindApps...)
		conflicts = append(conflicts, kindConflicts...)
		if writeProduct {
			product[kind] = next
		}
	}
	return head, product, apps, conflicts
}

func reduceKind(current Canonical, kind string, variants []Variant, sample Sample, policy Policy) (Canonical, bool, []Application, []Conflict) {
	key := sample.ChannelID + "/" + kind
	identified := identifiedVariants(variants)
	if len(identified) == 0 {
		return current, false, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "RAW_RETAINED"}}, nil
	}
	chosen, ok, conflict := chooseIdentity(identified)
	if !ok {
		return current, false, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CONFLICT"}}, []Conflict{conflict}
	}
	if rejected, next, apps, conflicts := rejectPhotoChange(current, kind, key, Identity(chosen), sample); rejected {
		return next, false, apps, conflicts
	}
	return applyPhotoChange(current, key, chosen, sample, policy)
}

func rejectPhotoChange(current Canonical, kind, key, identity string, sample Sample) (bool, Canonical, []Application, []Conflict) {
	if current.Identity != "" && current.EffectiveAt != nil && sample.EffectiveAt.Before(*current.EffectiveAt) {
		return true, current, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "OLDER_RETAINED"}}, nil
	}
	if current.Identity != "" && current.EffectiveAt != nil && sample.EffectiveAt.Equal(*current.EffectiveAt) && current.Identity != identity {
		return true, current, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CONFLICT"}}, []Conflict{{
			FieldName:            kind,
			ExistingValueSHA256:  contract.SHA256Hex([]byte(current.Identity)),
			AttemptedValueSHA256: contract.SHA256Hex([]byte(identity)),
		}}
	}
	if current.Identity == identity {
		resetCandidate(&current)
		return true, current, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CANONICAL_UNCHANGED"}}, nil
	}
	return false, current, nil, nil
}

func applyPhotoChange(current Canonical, key string, chosen Variant, sample Sample, policy Policy) (Canonical, bool, []Application, []Conflict) {
	identity := Identity(chosen)
	if !policy.ChangeEnabled() {
		return current, false, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CHANGE_DISABLED"}}, nil
	}
	if !sample.Complete {
		return current, false, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CHANGE_PENDING"}}, nil
	}
	if current.LastAt != nil && current.LastAt.Equal(sample.ScheduledFor) && current.Candidate == identity {
		return current, false, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "REPLAY"}}, nil
	}
	trackPhotoCandidate(&current, chosen, identity, sample)
	if current.Slots < policy.ChangeMinObservations ||
		current.FirstRx == nil ||
		sample.ReceivedAt.Before(current.FirstRx.Add(policy.ChangeStability)) {
		return current, false, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CHANGE_PENDING"}}, nil
	}
	current.Identity = identity
	current.URL = chosen.URL
	current.Width = chosen.Width
	current.Height = chosen.Height
	current.EffectiveAt = copyTime(sample.EffectiveAt)
	resetCandidate(&current)
	return current, true, []Application{{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "APPLIED"}}, nil
}

func trackPhotoCandidate(current *Canonical, chosen Variant, identity string, sample Sample) {
	if current.Candidate != identity {
		current.Candidate = identity
		current.CandidateURL = chosen.URL
		current.CandidateW = chosen.Width
		current.CandidateH = chosen.Height
		current.Slots = 1
		current.FirstAt = copyTime(sample.ScheduledFor)
		current.LastAt = copyTime(sample.ScheduledFor)
		current.FirstRx = copyTime(sample.ReceivedAt)
		return
	}
	current.Slots++
	current.LastAt = copyTime(sample.ScheduledFor)
	current.CandidateURL = chosen.URL
	current.CandidateW = chosen.Width
	current.CandidateH = chosen.Height
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
		if Identity(variants[i]) == "" {
			continue
		}
		identified = append(identified, variants[i])
	}
	return identified
}

func chooseIdentity(variants []Variant) (Variant, bool, Conflict) {
	chosen := variants[0]
	for i := 1; i < len(variants); i++ {
		if Identity(variants[i]) != Identity(chosen) {
			return Variant{}, false, Conflict{
				FieldName:            chosen.Kind,
				ExistingValueSHA256:  contract.SHA256Hex([]byte(Identity(chosen))),
				AttemptedValueSHA256: contract.SHA256Hex([]byte(Identity(variants[i]))),
			}
		}
		if variants[i].Width*variants[i].Height > chosen.Width*chosen.Height {
			chosen = variants[i]
		}
	}
	return chosen, true, Conflict{}
}

func resetCandidate(current *Canonical) {
	current.Candidate = ""
	current.CandidateURL = ""
	current.CandidateW = 0
	current.CandidateH = 0
	current.Slots = 0
	current.FirstAt = nil
	current.LastAt = nil
	current.FirstRx = nil
}

func copyTime(value time.Time) *time.Time {
	copied := value.UTC()
	return &copied
}
