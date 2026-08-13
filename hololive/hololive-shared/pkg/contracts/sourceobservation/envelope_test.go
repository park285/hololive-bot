package sourceobservation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCanonicalizeJSONIsStableAcrossObjectKeyOrder(t *testing.T) {
	left, err := CanonicalizeJSON([]byte(`{"b":2,"a":{"z":1,"y":[3,2,1]}}`))
	if err != nil {
		t.Fatalf("canonicalize left: %v", err)
	}
	right, err := CanonicalizeJSON([]byte(` { "a" : { "y" : [3,2,1], "z" : 1 }, "b" : 2 } `))
	if err != nil {
		t.Fatalf("canonicalize right: %v", err)
	}
	if string(left) != string(right) {
		t.Fatalf("canonical payload mismatch: %s != %s", left, right)
	}
	if SHA256Hex(left) != SHA256Hex(right) {
		t.Fatal("canonical hashes differ")
	}
}

func TestEnvelopeValidateAcceptsCommunityV1(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	payload := CommunityPayloadV1{
		ChannelID:   "UC_TEST",
		CollectedAt: now,
		Posts: []CommunityPostV1{{
			PostID:       "post-1",
			ChannelID:    "UC_TEST",
			AuthorName:   "Author",
			ContentText:  "hello",
			LikeCount:    1,
			CommentCount: 2,
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	canonical, err := CanonicalizeJSON(raw)
	if err != nil {
		t.Fatalf("canonicalize payload: %v", err)
	}
	envelope := Envelope{
		SourceKind:      SourceKindYouTubeCommunity,
		SourceKey:       "UC_TEST",
		ObservationKey: "post-1",
		SchemaVersion:  CommunitySchemaVersionV1,
		Generation:     1,
		ObservedAt:     now,
		Completeness:   CompletenessCompleteWindow,
		Continuity:     ContinuityContiguous,
		Payload:        raw,
		PayloadSHA256:  SHA256Hex(canonical),
	}
	if err := envelope.Validate(); err != nil {
		t.Fatalf("validate envelope: %v", err)
	}
}

func TestEnvelopeValidateRejectsHashMismatch(t *testing.T) {
	envelope := validTestEnvelope(t)
	envelope.PayloadSHA256 = strings.Repeat("0", 64)
	if err := envelope.Validate(); err == nil {
		t.Fatal("expected hash mismatch")
	}
}

func TestEnvelopeValidateRejectsSourceKeyMismatch(t *testing.T) {
	envelope := validTestEnvelope(t)
	envelope.SourceKey = "UC_OTHER"
	if err := envelope.Validate(); err == nil {
		t.Fatal("expected source key mismatch")
	}
}

func TestEnvelopeValidateRejectsDuplicatePostIDs(t *testing.T) {
	envelope := validTestEnvelope(t)
	var payload CommunityPayloadV1
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	payload.Posts = append(payload.Posts, payload.Posts[0])
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	canonical, err := CanonicalizeJSON(raw)
	if err != nil {
		t.Fatalf("canonicalize payload: %v", err)
	}
	envelope.Payload = raw
	envelope.PayloadSHA256 = SHA256Hex(canonical)
	if err := envelope.Validate(); err == nil {
		t.Fatal("expected duplicate post rejection")
	}
}

func TestEnvelopeValidateRejectsUnsupportedSchema(t *testing.T) {
	envelope := validTestEnvelope(t)
	envelope.SchemaVersion = 2
	if err := envelope.Validate(); err == nil {
		t.Fatal("expected unsupported schema rejection")
	}
}

func validTestEnvelope(t *testing.T) Envelope {
	t.Helper()
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(CommunityPayloadV1{
		ChannelID:   "UC_TEST",
		CollectedAt: now,
		Posts: []CommunityPostV1{{
			PostID:    "post-1",
			ChannelID: "UC_TEST",
		}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	canonical, err := CanonicalizeJSON(raw)
	if err != nil {
		t.Fatalf("canonicalize payload: %v", err)
	}
	return Envelope{
		SourceKind:      SourceKindYouTubeCommunity,
		SourceKey:       "UC_TEST",
		ObservationKey: "post-1",
		SchemaVersion:  CommunitySchemaVersionV1,
		Generation:     1,
		ObservedAt:     now,
		Completeness:   CompletenessCompleteWindow,
		Continuity:     ContinuityContiguous,
		Payload:        raw,
		PayloadSHA256:  SHA256Hex(canonical),
	}
}

func TestEnvelopeValidateRejectsUnknownPayloadField(t *testing.T) {
	envelope := validTestEnvelope(t)
	raw := []byte(`{"channel_id":"UC_TEST","collected_at":"2026-08-13T08:00:00Z","posts":[],"unexpected":true}`)
	canonical, err := CanonicalizeJSON(raw)
	if err != nil {
		t.Fatalf("canonicalize payload: %v", err)
	}
	envelope.Payload = raw
	envelope.PayloadSHA256 = SHA256Hex(canonical)
	if err := envelope.Validate(); err == nil {
		t.Fatal("expected unknown field rejection")
	}
}

func TestEnvelopeValidateRejectsNonHTTPSImage(t *testing.T) {
	envelope := validTestEnvelope(t)
	var payload CommunityPayloadV1
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	payload.Posts[0].Images = []Thumbnail{{URL: "http://example.test/image.jpg", Width: 100, Height: 100}}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	canonical, err := CanonicalizeJSON(raw)
	if err != nil {
		t.Fatalf("canonicalize payload: %v", err)
	}
	envelope.Payload = raw
	envelope.PayloadSHA256 = SHA256Hex(canonical)
	if err := envelope.Validate(); err == nil {
		t.Fatal("expected non-HTTPS image rejection")
	}
}
