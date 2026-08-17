package sourceobservation

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestERR001AllCollectionErrorCodesExactSortedSet(t *testing.T) {
	t.Parallel()
	want := []CollectionErrorCode{
		ErrorCollectionCanceled,
		ErrorCollectionFailed,
		ErrorInternalInvariant,
		ErrorCollectionTimeout,
		ErrorConfiguration,
		ErrorCooldown,
		ErrorHelperBusy,
		ErrorHelperProtocolMismatch,
		ErrorObservationCollision,
		ErrorParserDrift,
		ErrorPublishRejected,
		ErrorRenewFailedRelease,
		ErrorResponseTooLarge,
		ErrorShutdownRelease,
		ErrorSupersededRelease,
		ErrorTargetRosterTooLarge,
	}
	slices.Sort(want)
	got := AllCollectionErrorCodes()
	if !slices.Equal(got, want) {
		t.Fatalf("AllCollectionErrorCodes() = %#v, want %#v", got, want)
	}
	if len(got) != 16 {
		t.Fatalf("AllCollectionErrorCodes length = %d, want 16", len(got))
	}
	got[0] = "mutated"
	fresh := AllCollectionErrorCodes()
	if len(fresh) == 0 {
		t.Fatal("AllCollectionErrorCodes returned an empty vocabulary")
	}
	if fresh[0] == "mutated" {
		t.Fatal("AllCollectionErrorCodes leaked internal state")
	}
}

func TestERR002TerminalCodeSetsPartitionPersistedVocabulary(t *testing.T) {
	t.Parallel()
	deferable := DeferableCollectionErrorCodes()
	releasable := ReleasableCollectionErrorCodes()
	complete := CompletesWithErrorCodes()
	if overlap := intersectCodes(deferable, releasable); len(overlap) > 0 {
		t.Fatalf("defer ∩ release = %#v", overlap)
	}
	if overlap := intersectCodes(deferable, complete); len(overlap) > 0 {
		t.Fatalf("defer ∩ complete-with-error = %#v", overlap)
	}
	if overlap := intersectCodes(releasable, complete); len(overlap) > 0 {
		t.Fatalf("release ∩ complete-with-error = %#v", overlap)
	}
	union := append(append(slices.Clone(deferable), releasable...), complete...)
	slices.Sort(union)
	if !slices.Equal(union, AllCollectionErrorCodes()) {
		t.Fatalf("union = %#v, want %#v", union, AllCollectionErrorCodes())
	}
	durable := append(slices.Clone(DeferFailureTuples()), CompleteErrorFailureTuples()...)
	slices.SortFunc(durable, compareFailureTuple)
	if !slices.Equal(durable, AllDurableFailureTuples()) {
		t.Fatalf("durable union = %#v, want %#v", durable, AllDurableFailureTuples())
	}
}

func TestAPI001ArbitraryCollectionErrorCodeRejectedByTerminalAPI(t *testing.T) {
	t.Parallel()
	code := CollectionErrorCode("not_a_real_code")
	if code.Valid() || code.Deferable() || code.Releasable() || code.CompletesWithError() {
		t.Fatal("arbitrary code must be rejected without a constructor")
	}
	if _, err := NewFailureDiagnostic(code, ClassTransient, "detail"); err == nil {
		t.Fatal("NewFailureDiagnostic accepted arbitrary code")
	}
	diagnostic := FailureDiagnostic{code: code, class: ClassTransient, detail: "detail"}
	if err := diagnostic.Validate(); err == nil {
		t.Fatal("Validate accepted arbitrary code")
	}
	if err := diagnostic.ValidateFor(TerminalDefer); err == nil {
		t.Fatal("ValidateFor accepted arbitrary code")
	}
}

func TestAPI008FailureDiagnosticHasNoPublicMutableSurface(t *testing.T) {
	t.Parallel()
	diagnostic, err := NewFailureDiagnostic(ErrorCollectionFailed, ClassTransient, "network reset")
	if err != nil {
		t.Fatal(err)
	}
	_ = diagnostic.Detail() + "mutated"
	if diagnostic.Detail() != "network reset" {
		t.Fatal("Detail mutation leaked into FailureDiagnostic")
	}
	if diagnostic.Code() != ErrorCollectionFailed || diagnostic.Class() != ClassTransient {
		t.Fatalf("getters = %s/%s", diagnostic.Code(), diagnostic.Class())
	}
}

func TestERR007RejectsImpossibleCodeClassDiagnosticTuple(t *testing.T) {
	t.Parallel()
	if _, err := NewFailureDiagnostic(ErrorCollectionFailed, ClassTimeout, "wrong class"); err == nil {
		t.Fatal("collection_failed/TIMEOUT must be rejected")
	}
	if _, err := NewFailureDiagnostic(ErrorShutdownRelease, ClassCanceled, "release"); err == nil {
		t.Fatal("release reason must not construct a durable diagnostic")
	}
	if _, err := NewFailureDiagnostic(ErrorObservationCollision, ClassTransient, "collision"); err == nil {
		t.Fatal("observation_collision/TRANSIENT must be rejected")
	}
	valid, err := NewFailureDiagnostic(ErrorCollectionFailed, ClassTransient, "ok")
	if err != nil {
		t.Fatal(err)
	}
	if err := valid.ValidateFor(TerminalRelease); err == nil {
		t.Fatal("ValidateFor(TerminalRelease) must always fail")
	}
	if err := valid.ValidateFor(TerminalCompleteError); err == nil {
		t.Fatal("defer tuple must not validate for complete-with-error")
	}
}

func TestDefaultFailureClassUsesUniqueOrSection53Default(t *testing.T) {
	t.Parallel()
	if class, ok := DefaultFailureClass(ErrorParserDrift); !ok || class != ClassDataContract {
		t.Fatalf("parser_drift default = %s ok=%t", class, ok)
	}
	if class, ok := DefaultFailureClass(ErrorCollectionFailed); !ok || class != ClassTransient {
		t.Fatalf("collection_failed default = %s ok=%t", class, ok)
	}
	if class, ok := DefaultFailureClass(ErrorPublishRejected); !ok || class != ClassTransient {
		t.Fatalf("publish_rejected default = %s ok=%t", class, ok)
	}
	if _, ok := DefaultFailureClass("not_a_real_code"); ok {
		t.Fatal("unknown code must not have a default class")
	}
}

func TestERR015NewWriterRejectsLegacyAndImplementationClassNames(t *testing.T) {
	t.Parallel()
	for _, class := range []FailureClass{"legacy_collector", "RpcResponseError", "InnertubeError", "HelperError", "unknown_error"} {
		if _, err := NewFailureDiagnostic(ErrorCollectionFailed, class, "historical"); err == nil {
			t.Fatalf("NewFailureDiagnostic accepted historical class %q", class)
		}
		if class.Valid() {
			t.Fatalf("historical class %q must not enter closed vocabulary", class)
		}
	}
}

func TestFailureDiagnosticRejectsEmptyNULAndOverlongDetail(t *testing.T) {
	t.Parallel()
	if _, err := NewFailureDiagnostic(ErrorCooldown, ClassCooldown, "  "); err == nil {
		t.Fatal("blank detail must be rejected")
	}
	if _, err := NewFailureDiagnostic(ErrorCooldown, ClassCooldown, "bad\x00detail"); err == nil {
		t.Fatal("NUL detail must be rejected")
	}
	if _, err := NewFailureDiagnostic(ErrorCooldown, ClassCooldown, strings.Repeat("a", maxFailureDetailBytes+1)); err == nil {
		t.Fatal("overlong detail must be rejected")
	}
	if !utf8.ValidString(strings.Repeat("가", 10)) {
		t.Fatal("fixture is invalid")
	}
	got, err := NewFailureDiagnostic(ErrorCooldown, ClassCooldown, "  retry later  ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Detail() != "retry later" {
		t.Fatalf("detail = %q", got.Detail())
	}
}

func TestTupleGettersReturnSortedDefensiveCopies(t *testing.T) {
	t.Parallel()
	tuples := DeferFailureTuples()
	if !slices.IsSortedFunc(tuples, compareFailureTuple) {
		t.Fatalf("DeferFailureTuples is unsorted: %#v", tuples)
	}
	if len(tuples) == 0 {
		t.Fatal("DeferFailureTuples returned an empty vocabulary")
	}
	tuples[0].Code = "mutated"
	freshDefer := DeferFailureTuples()
	if len(freshDefer) == 0 {
		t.Fatal("DeferFailureTuples returned an empty defensive copy")
	}
	if freshDefer[0].Code == "mutated" {
		t.Fatal("DeferFailureTuples leaked internal state")
	}
	complete := CompleteErrorFailureTuples()
	if len(complete) == 0 {
		t.Fatal("CompleteErrorFailureTuples returned an empty vocabulary")
	}
	complete[0].Class = "mutated"
	freshComplete := CompleteErrorFailureTuples()
	if len(freshComplete) == 0 {
		t.Fatal("CompleteErrorFailureTuples returned an empty defensive copy")
	}
	if freshComplete[0].Class == "mutated" {
		t.Fatal("CompleteErrorFailureTuples leaked internal state")
	}
}

func intersectCodes(left, right []CollectionErrorCode) []CollectionErrorCode {
	rightSet := setFromCodes(right)
	var overlap []CollectionErrorCode
	for _, code := range left {
		if _, ok := rightSet[code]; ok {
			overlap = append(overlap, code)
		}
	}
	return overlap
}
