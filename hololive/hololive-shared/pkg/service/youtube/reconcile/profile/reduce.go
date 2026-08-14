package profile

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func Reduce(state State, evidence Evidence, policy Policy) (Decision, error) {
	if evidence.Sample.ChannelID == "" {
		return Decision{}, fmt.Errorf("channel profile reducer received empty channel id")
	}
	sample := evidence.Sample
	sample.Provider = evidence.Provider
	sample.ObservationID = evidence.ObservationID
	head := state.Head
	head.ChannelID = sample.ChannelID
	apps := make([]Application, 0, 4)
	conflicts := make([]Conflict, 0, 2)
	changed := false
	changed = reduceHandle(&head.Handle, sample, &apps, &conflicts) || changed
	changed = reduceClearable(&head.Description, sample.Description, "description", sample, policy, &apps, &conflicts) || changed
	changed = reduceClearable(&head.Country, sample.Country, "country", sample, policy, &apps, &conflicts) || changed
	changed = reduceJoined(&head.JoinedDate, sample, &apps, &conflicts) || changed
	if len(apps) == 0 {
		apps = append(apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID, Decision: "RAW_RETAINED",
		})
	}
	return Decision{
		Sample:       &sample,
		Head:         head,
		WriteHead:    changed,
		Conflicts:    conflicts,
		Applications: apps,
	}, nil
}

func reduceHandle(current *CanonicalField, sample Sample, apps *[]Application, conflicts *[]Conflict) bool {
	if !sample.Handle.Present {
		return false
	}
	value, ok := normalizeHandle(sample.Handle.Value)
	if !ok {
		*apps = append(*apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/handle", Decision: "INVALID_RETAINED",
		})
		return false
	}
	return applyNewer(current, value, "handle", sample, apps, conflicts)
}

func reduceClearable(
	current *CanonicalField,
	incoming Field,
	name string,
	sample Sample,
	policy Policy,
	apps *[]Application,
	conflicts *[]Conflict,
) bool {
	if !incoming.Present {
		return false
	}
	value, ok := normalizeClearable(name, incoming.Value)
	if !ok {
		*apps = append(*apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/" + name, Decision: "INVALID_RETAINED",
		})
		return false
	}
	if value != "" {
		resetEmpty(current)
		return applyNewer(current, value, name, sample, apps, conflicts)
	}
	if current.Set && current.Value == "" {
		return trackEmpty(current, name, sample, policy, apps)
	}
	if !current.Set {
		return trackEmpty(current, name, sample, policy, apps)
	}
	if !policy.ClearEnabled() {
		*apps = append(*apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/" + name, Decision: "CLEAR_DISABLED",
		})
		return false
	}
	return trackEmpty(current, name, sample, policy, apps)
}

func reduceJoined(current *CanonicalField, sample Sample, apps *[]Application, conflicts *[]Conflict) bool {
	if !sample.JoinedDate.Present {
		return false
	}
	value, ok := parseJoinedDate(sample.JoinedDate.Value)
	if !ok {
		*apps = append(*apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/joined_date", Decision: "INVALID_RETAINED",
		})
		return false
	}
	if current.Set && current.Value != value {
		if current.EffectiveAt != nil && sample.EffectiveAt.Before(*current.EffectiveAt) {
			*apps = append(*apps, Application{
				EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/joined_date", Decision: "OLDER_RETAINED",
			})
			return false
		}
		*conflicts = append(*conflicts, Conflict{
			FieldName:            "joined_date",
			ExistingValueSHA256:  contract.SHA256Hex([]byte(current.Value)),
			AttemptedValueSHA256: contract.SHA256Hex([]byte(value)),
		})
		*apps = append(*apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/joined_date", Decision: "CONFLICT",
		})
		return false
	}
	return applyNewer(current, value, "joined_date", sample, apps, conflicts)
}

func applyNewer(
	current *CanonicalField,
	value, name string,
	sample Sample,
	apps *[]Application,
	conflicts *[]Conflict,
) bool {
	if current.Set && current.EffectiveAt != nil && sample.EffectiveAt.Before(*current.EffectiveAt) {
		*apps = append(*apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/" + name, Decision: "OLDER_RETAINED",
		})
		return false
	}
	if current.Set && current.EffectiveAt != nil && sample.EffectiveAt.Equal(*current.EffectiveAt) && current.Value != value {
		*conflicts = append(*conflicts, Conflict{
			FieldName:            name,
			ExistingValueSHA256:  contract.SHA256Hex([]byte(current.Value)),
			AttemptedValueSHA256: contract.SHA256Hex([]byte(value)),
		})
		*apps = append(*apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/" + name, Decision: "CONFLICT",
		})
		return false
	}
	if current.Set && current.Value == value {
		*apps = append(*apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/" + name, Decision: "REPLAY",
		})
		return false
	}
	current.Set = true
	current.Value = value
	current.EffectiveAt = copyTime(sample.EffectiveAt)
	*apps = append(*apps, Application{
		EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/" + name, Decision: "APPLIED",
	})
	return true
}

func trackEmpty(current *CanonicalField, name string, sample Sample, policy Policy, apps *[]Application) bool {
	if !sample.Complete {
		*apps = append(*apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/" + name, Decision: "CLEAR_PENDING",
		})
		return false
	}
	if current.EmptyLastAt != nil && current.EmptyLastAt.Equal(sample.ScheduledFor) {
		*apps = append(*apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/" + name, Decision: "REPLAY",
		})
		return false
	}
	if current.EmptyFirstAt == nil {
		current.EmptySlots = 1
		current.EmptyFirstAt = copyTime(sample.ScheduledFor)
		current.EmptyLastAt = copyTime(sample.ScheduledFor)
		current.EmptyFirstRx = copyTime(sample.ReceivedAt)
	} else {
		current.EmptySlots++
		current.EmptyLastAt = copyTime(sample.ScheduledFor)
	}
	if !policy.ClearEnabled() {
		*apps = append(*apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/" + name, Decision: "CLEAR_DISABLED",
		})
		return true
	}
	if current.EmptySlots < policy.ClearMinObservations ||
		current.EmptyFirstRx == nil ||
		sample.ReceivedAt.Before(current.EmptyFirstRx.Add(policy.ClearStability)) {
		*apps = append(*apps, Application{
			EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/" + name, Decision: "CLEAR_PENDING",
		})
		return true
	}
	current.Set = true
	current.Value = ""
	current.EffectiveAt = copyTime(sample.EffectiveAt)
	resetEmpty(current)
	*apps = append(*apps, Application{
		EntityKind: "youtube_channel_profile", EntityKey: sample.ChannelID + "/" + name, Decision: "CLEARED",
	})
	return true
}

func resetEmpty(current *CanonicalField) {
	current.EmptySlots = 0
	current.EmptyFirstAt = nil
	current.EmptyLastAt = nil
	current.EmptyFirstRx = nil
}

func normalizeHandle(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > 256 || strings.ContainsAny(trimmed, " \t\r\n") {
		return "", false
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return trimmed, true
}

func normalizeClearable(name, value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if name == "country" {
		if trimmed == "" {
			return "", true
		}
		upper := strings.ToUpper(trimmed)
		if len(upper) < 2 || len(upper) > 50 {
			return "", false
		}
		for _, r := range upper {
			if unicode.IsControl(r) {
				return "", false
			}
		}
		return upper, true
	}
	if !utf8.ValidString(trimmed) || len(trimmed) > 4096 {
		return "", false
	}
	return trimmed, true
}

func parseJoinedDate(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	if parsed, err := time.Parse("2006-01-02", trimmed); err == nil {
		return parsed.UTC().Format("2006-01-02"), true
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed.UTC().Format("2006-01-02"), true
	}
	unix, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || unix <= 0 {
		return "", false
	}
	return time.Unix(unix, 0).UTC().Format("2006-01-02"), true
}

func copyTime(value time.Time) *time.Time {
	copied := value.UTC()
	return &copied
}
