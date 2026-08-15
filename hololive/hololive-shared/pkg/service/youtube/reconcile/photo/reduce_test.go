package photo

import (
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestReduceCopiesInputBackingStorage(t *testing.T) {
	t.Parallel()
	effective := t1()
	originalEffective := effective
	state := State{ChannelID: "UC_TEST", Head: Head{ChannelID: "UC_TEST", Kinds: map[string]Canonical{
		"avatar": {Identity: "id:media-1", URL: "https://img.test/a.jpg", EffectiveAt: &effective},
	}}}
	evidence := photoAt(1, t2(), Variant{Kind: "avatar", URL: "https://img.test/a.jpg", StableMediaID: "media-1"})
	decision, err := Reduce(state, evidence, Policy{})
	if err != nil {
		t.Fatal(err)
	}

	evidence.Sample.Variants[0].URL = "https://mutated.test/input.jpg"
	if decision.Sample.Variants[0].URL != "https://img.test/a.jpg" {
		t.Fatal("decision shares evidence variants")
	}
	canonical := decision.Head.Kinds["avatar"]
	*canonical.EffectiveAt = effective.Add(time.Hour)
	decision.Head.Kinds["avatar"] = canonical
	if !state.Head.Kinds["avatar"].EffectiveAt.Equal(originalEffective) {
		t.Fatal("decision shares state canonical pointer")
	}
}

func TestReduceSameIdentityNewURLCreatesNoChangeEvent(t *testing.T) {
	t.Parallel()
	first := photoAt(1, t1(), Variant{
		Kind: "avatar", URL: "https://img.test/a.jpg?s=88", Width: 88, Height: 88, StableMediaID: "media-1",
	})
	second := photoAt(2, t2(), Variant{
		Kind: "avatar", URL: "https://img.test/a.jpg?s=800", Width: 800, Height: 800, StableMediaID: "media-1",
	})
	enabled := Policy{ChangeMinObservations: 2, ChangeStability: time.Hour}
	got := mustReduceAll(t, []Evidence{first, firstAtLater(), second}, enabled)
	if len(got.WriteProduct) != 0 {
		t.Fatalf("same identity wrote product: %#v", got.WriteProduct)
	}
	if !hasDecision(got, "CANONICAL_UNCHANGED") {
		t.Fatalf("applications = %#v", got.Applications)
	}
}

func TestReducePhotoWithoutStableIDOrFingerprintCannotChangeCanonical(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, []Evidence{photoAt(1, t1(), Variant{
		Kind: "avatar", URL: "https://img.test/a.jpg?s=88", Width: 88, Height: 88,
	})}, Policy{ChangeMinObservations: 2, ChangeStability: time.Hour})
	if got.Head.Kinds["avatar"].Identity != "" || len(got.WriteProduct) != 0 {
		t.Fatalf("unidentified photo changed canonical: %#v", got)
	}
}

func TestReduceDifferentPhotoIdentityRequiresStabilityThreshold(t *testing.T) {
	t.Parallel()
	first := photoAt(1, t1(), Variant{
		Kind: "avatar", URL: "https://img.test/a.jpg", Width: 88, Height: 88, StableMediaID: "media-1",
	})
	next := photoAt(2, t2(), Variant{
		Kind: "avatar", URL: "https://img.test/b.jpg", Width: 88, Height: 88, StableMediaID: "media-2",
	})
	disabled := mustReduceAll(t, []Evidence{first, firstAtLater(), next}, Policy{})
	if disabled.Head.Kinds["avatar"].Identity != "" {
		t.Fatalf("disabled change must keep empty canonical: %#v", disabled.Head.Kinds["avatar"])
	}
	pending := mustReduceAll(t, []Evidence{first, firstAtLater(), next}, Policy{ChangeMinObservations: 2, ChangeStability: time.Hour})
	if pending.Head.Kinds["avatar"].Identity != "id:media-1" {
		t.Fatalf("first stable identity = %#v", pending.Head.Kinds["avatar"])
	}
	later := photoAt(3, t2().Add(2*time.Hour), Variant{
		Kind: "avatar", URL: "https://img.test/b.jpg", Width: 88, Height: 88, StableMediaID: "media-2",
	})
	changed := mustReduceAll(t, []Evidence{first, firstAtLater(), next, later}, Policy{ChangeMinObservations: 2, ChangeStability: time.Hour})
	if changed.Head.Kinds["avatar"].Identity != "id:media-2" {
		t.Fatalf("stable identity change = %#v", changed.Head.Kinds["avatar"])
	}
}

func TestReduceDoesNotSynthesizeFingerprintFromURL(t *testing.T) {
	t.Parallel()
	variant := Variant{Kind: "avatar", URL: "https://img.test/a.jpg?s=88", Width: 88, Height: 88}
	if Identity(&variant) != "" {
		t.Fatal("collector URL must not become a synthesized fingerprint")
	}
}

func TestReducePhotoPermutationsYieldSameProjection(t *testing.T) {
	t.Parallel()
	a := photoAt(1, t1(), Variant{
		Kind: "avatar", URL: "https://img.test/a.jpg?keep=1", Width: 88, Height: 88, StableMediaID: "media-1",
	})
	b := photoAt(2, t2(), Variant{
		Kind: "banner", URL: "https://img.test/b.jpg?keep=1", Width: 100, Height: 20, ContentFingerprint: strings.Repeat("ab", 32),
	})
	policy := Policy{ChangeMinObservations: 2, ChangeStability: time.Second}
	forward := mustReduceAll(t, []Evidence{a, aAt(3, t1().Add(2*time.Hour), a.Sample.Variants[0]), b, bAt(4, t2().Add(2*time.Hour), b.Sample.Variants[0])}, policy)
	reverse := mustReduceAll(t, []Evidence{b, bAt(4, t2().Add(2*time.Hour), b.Sample.Variants[0]), a, aAt(3, t1().Add(2*time.Hour), a.Sample.Variants[0])}, policy)
	if forward.Head.Kinds["avatar"].Identity != reverse.Head.Kinds["avatar"].Identity ||
		forward.Head.Kinds["banner"].Identity != reverse.Head.Kinds["banner"].Identity {
		t.Fatalf("permutation projection differs: %#v vs %#v", forward.Head.Kinds, reverse.Head.Kinds)
	}
	if !strings.Contains(forward.Head.Kinds["avatar"].URL, "keep=1") {
		t.Fatalf("reconciler stripped query params: %s", forward.Head.Kinds["avatar"].URL)
	}
}

func t1() time.Time { return time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC) }
func t2() time.Time { return time.Date(2026, 8, 14, 7, 0, 0, 0, time.UTC) }

func photoAt(id int64, at time.Time, variants ...Variant) Evidence {
	return aAt(id, at, variants...)
}

func firstAtLater() Evidence {
	return aAt(11, t1().Add(2*time.Hour), Variant{
		Kind: "avatar", URL: "https://img.test/a.jpg", Width: 88, Height: 88, StableMediaID: "media-1",
	})
}

func aAt(id int64, at time.Time, variants ...Variant) Evidence {
	return Evidence{
		ObservationID: id,
		Provider:      contract.ProviderYouTubeJS,
		Sample: Sample{
			ChannelID: "UC_TEST", Variants: variants, Complete: true,
			ObservationID: id, ScheduledFor: at, EffectiveAt: at, ReceivedAt: at,
		},
	}
}

func bAt(id int64, at time.Time, variants ...Variant) Evidence {
	got := aAt(id, at, variants...)
	got.Provider = contract.ProviderHolodex
	got.Sample.Provider = contract.ProviderHolodex
	return got
}

func mustReduceAll(t *testing.T, evidence []Evidence, policy Policy) *Decision {
	t.Helper()
	current := State{}
	var decision Decision
	for i := range evidence {
		next, err := Reduce(current, evidence[i], policy)
		if err != nil {
			t.Fatalf("reduce[%d]: %v", i, err)
		}
		decision = next
		current.Head = next.Head
		current.ChannelID = evidence[i].Sample.ChannelID
	}
	return &decision
}

func hasDecision(decision *Decision, want string) bool {
	for _, app := range decision.Applications {
		if app.Decision == want {
			return true
		}
	}
	return false
}
