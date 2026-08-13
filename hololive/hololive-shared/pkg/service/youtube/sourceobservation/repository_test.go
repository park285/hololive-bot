package sourceobservation

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestNewLeaseTokenReturnsLowercaseSHA256Width(t *testing.T) {
	first, err := newLeaseToken()
	if err != nil {
		t.Fatalf("create first token: %v", err)
	}
	second, err := newLeaseToken()
	if err != nil {
		t.Fatalf("create second token: %v", err)
	}
	if !lowercaseHexToken(first) || !lowercaseHexToken(second) {
		t.Fatalf("invalid lease token shape: %q %q", first, second)
	}
	if first == second {
		t.Fatal("lease tokens unexpectedly match")
	}
}

func TestClaimOptionsValidateBounds(t *testing.T) {
	valid := ClaimOptions{
		SourceKind:    contract.SourceKindYouTubeCommunity,
		ConsumerName:  "youtube-community-processor",
		LeaseOwner:    "instance-a",
		Limit:         10,
		LeaseDuration: 30 * time.Second,
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("validate claim options: %v", err)
	}

	invalid := valid
	invalid.Limit = MaxClaimBatchSize + 1
	if err := invalid.validate(); err == nil {
		t.Fatal("expected claim limit rejection")
	}
	invalid = valid
	invalid.LeaseDuration = time.Millisecond
	if err := invalid.validate(); err == nil {
		t.Fatal("expected lease duration rejection")
	}
}

func TestCompletionValidateRejectsNonObjectParityDetail(t *testing.T) {
	completion := Completion{
		ConsumerName:       "youtube-community-processor",
		ObservationID:      1,
		SourceKind:         contract.SourceKindYouTubeCommunity,
		LeaseToken:         strings.Repeat("a", 64),
		ExpectedGeneration: 1,
		ParityStatus:       contract.ParityStatusMismatch,
		ParityDetail:       json.RawMessage(`["not-an-object"]`),
	}
	if err := completion.validate(); err == nil {
		t.Fatal("expected parity detail rejection")
	}
}

func TestRetryInputValidateBoundsErrorDetail(t *testing.T) {
	input := RetryInput{
		ObservationID: 1,
		SourceKind:    contract.SourceKindYouTubeCommunity,
		LeaseToken:    strings.Repeat("b", 64),
		Delay:         time.Second,
		ErrorCode:     "temporary_failure",
		ErrorDetail:   strings.Repeat("x", maxErrorTextBytes+1),
	}
	if err := input.validate(); err == nil {
		t.Fatal("expected oversized error detail rejection")
	}
}

func TestValidateFinalizeModeEnforcesAuthorityBoundary(t *testing.T) {
	if err := validateFinalizeMode(contract.AuthorityModeShadow, contract.ParityStatusMatch, false); err != nil {
		t.Fatalf("validate shadow finalize: %v", err)
	}
	if err := validateFinalizeMode(contract.AuthorityModeShadow, contract.ParityStatusNotChecked, false); err == nil {
		t.Fatal("expected missing shadow parity rejection")
	}
	if err := validateFinalizeMode(contract.AuthorityModeShadow, contract.ParityStatusMatch, true); err == nil {
		t.Fatal("expected shadow authoritative callback rejection")
	}
	if err := validateFinalizeMode(contract.AuthorityModeAuthoritative, contract.ParityStatusNotChecked, true); err != nil {
		t.Fatalf("validate authoritative finalize: %v", err)
	}
	if err := validateFinalizeMode(contract.AuthorityModeAuthoritative, contract.ParityStatusNotChecked, false); err == nil {
		t.Fatal("expected missing authoritative callback rejection")
	}
	if err := validateFinalizeMode(contract.AuthorityModeLegacy, contract.ParityStatusNotChecked, false); !errors.Is(err, ErrAuthorityInactive) {
		t.Fatalf("expected inactive authority, got %v", err)
	}
}

func TestObservationEnvelopePreservesClaimedIdentity(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	observation := Observation{
		SourceKind:      contract.SourceKindYouTubeCommunity,
		SourceKey:       "UC_TEST",
		ObservationKey: "post-1",
		SchemaVersion:  contract.CommunitySchemaVersionV1,
		Generation:     3,
		ObservedAt:     now,
		Completeness:   contract.CompletenessCompleteWindow,
		Continuity:     contract.ContinuityContiguous,
		Payload:        json.RawMessage(`{"channel_id":"UC_TEST"}`),
		PayloadSHA256:  strings.Repeat("a", 64),
	}
	envelope := observation.Envelope()
	if envelope.SourceKey != observation.SourceKey || envelope.Generation != observation.Generation {
		t.Fatalf("envelope identity changed: %#v", envelope)
	}
}
