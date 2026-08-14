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
	product := map[string]Canonical{}
	byKind, err := groupVariants(sample.Variants)
	if err != nil {
		return Decision{}, err
	}
	for _, kind := range []string{"avatar", "banner"} {
		variants := byKind[kind]
		if len(variants) == 0 {
			continue
		}
		current := head.Kinds[kind]
		next, writeProduct, kindApps, kindConflicts := reduceKind(current, kind, variants, sample, policy)
		head.Kinds[kind] = next
		apps = append(apps, kindApps...)
		conflicts = append(conflicts, kindConflicts...)
		if writeProduct {
			product[kind] = next
		}
	}
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

func reduceKind(current Canonical, kind string, variants []Variant, sample Sample, policy Policy) (Canonical, bool, []Application, []Conflict) {
	key := sample.ChannelID + "/" + kind
	identified, unidentified := splitIdentified(variants)
	apps := []Application{}
	if len(identified) == 0 {
		apps = append(apps, Application{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "RAW_RETAINED"})
		return current, false, apps, nil
	}
	chosen, ok, conflict := chooseIdentity(identified)
	if !ok {
		apps = append(apps, Application{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CONFLICT"})
		return current, false, apps, []Conflict{conflict}
	}
	_ = unidentified
	identity := Identity(chosen)
	if current.Identity != "" && current.EffectiveAt != nil && sample.EffectiveAt.Before(*current.EffectiveAt) {
		apps = append(apps, Application{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "OLDER_RETAINED"})
		return current, false, apps, nil
	}
	if current.Identity != "" && current.EffectiveAt != nil && sample.EffectiveAt.Equal(*current.EffectiveAt) && current.Identity != identity {
		apps = append(apps, Application{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CONFLICT"})
		return current, false, apps, []Conflict{{
			FieldName:            kind,
			ExistingValueSHA256:  contract.SHA256Hex([]byte(current.Identity)),
			AttemptedValueSHA256: contract.SHA256Hex([]byte(identity)),
		}}
	}
	if current.Identity == identity {
		resetCandidate(&current)
		apps = append(apps, Application{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CANONICAL_UNCHANGED"})
		return current, false, apps, nil
	}
	if !policy.ChangeEnabled() {
		apps = append(apps, Application{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CHANGE_DISABLED"})
		return current, false, apps, nil
	}
	if !sample.Complete {
		apps = append(apps, Application{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CHANGE_PENDING"})
		return current, false, apps, nil
	}
	if current.LastAt != nil && current.LastAt.Equal(sample.ScheduledFor) && current.Candidate == identity {
		apps = append(apps, Application{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "REPLAY"})
		return current, false, apps, nil
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
	} else {
		current.Slots++
		current.LastAt = copyTime(sample.ScheduledFor)
		current.CandidateURL = chosen.URL
		current.CandidateW = chosen.Width
		current.CandidateH = chosen.Height
	}
	if current.Slots < policy.ChangeMinObservations ||
		current.FirstRx == nil ||
		sample.ReceivedAt.Before(current.FirstRx.Add(policy.ChangeStability)) {
		apps = append(apps, Application{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "CHANGE_PENDING"})
		return current, false, apps, nil
	}
	current.Identity = identity
	current.URL = chosen.URL
	current.Width = chosen.Width
	current.Height = chosen.Height
	current.EffectiveAt = copyTime(sample.EffectiveAt)
	resetCandidate(&current)
	apps = append(apps, Application{EntityKind: "youtube_channel_photo", EntityKey: key, Decision: "APPLIED"})
	return current, true, apps, nil
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

func splitIdentified(variants []Variant) (identified, unidentified []Variant) {
	for i := range variants {
		if Identity(variants[i]) == "" {
			unidentified = append(unidentified, variants[i])
			continue
		}
		identified = append(identified, variants[i])
	}
	return identified, unidentified
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
