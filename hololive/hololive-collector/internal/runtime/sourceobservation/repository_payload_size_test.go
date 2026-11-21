package sourceobservation

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func postgresPayloadSize(t *testing.T, pool *pgxpool.Pool, raw []byte) int {
	t.Helper()

	var size int

	if err := pool.QueryRow(t.Context(), `SELECT octet_length($1::jsonb::text)`, string(raw)).Scan(&size); err != nil {
		t.Fatal(err)
	}

	return size
}

func TestPayloadStorageLimitMatchesPostgreSQL(t *testing.T) {
	pool := dbtest.NewPool(t)

	for _, fixture := range []string{
		`[]`, `{}`, `[[[],{}],{"empty":[],"other":{}},true,false,null]`,
		`[9007199254740991,-9007199254740991,-0,1e3,10.00]`,
		`{"한글😀":"日本語,:\"\\\b\f\n\r\t\u001f/","escaped\"key":":"}`,
	} {
		t.Run(fixture, func(t *testing.T) {
			base := []byte(`{"fixture":` + fixture + `,"padding":""}`)

			canonical, err := contract.CanonicalizeJSON(base)
			if err != nil {
				t.Fatal(err)
			}

			padding := contract.MaxPayloadBytes - postgresPayloadSize(t, pool, canonical)

			for _, extra := range []int{0, 1} {
				raw := jsontext.Value(`{"fixture":` + fixture + `,"padding":"` + strings.Repeat("x", padding+extra) + `"}`)

				canonical, err = contract.CanonicalizeJSON(raw)
				if err != nil {
					t.Fatal(err)
				}

				if got := postgresPayloadSize(t, pool, canonical); got != contract.MaxPayloadBytes+extra {
					t.Fatalf("PostgreSQL size=%d", got)
				}

				payload, err := contract.MarshalPayloadV1(raw)
				if extra == 0 {
					if err != nil || !bytes.Equal(payload, canonical) || contract.SHA256Hex(payload) != contract.SHA256Hex(canonical) {
						t.Fatalf("exact boundary changed: %v", err)
					}
				} else if !errors.Is(err, contract.ErrPayloadTooLarge) {
					t.Fatalf("one-over accepted: %v", err)
				}
			}
		})
	}
}

func TestPublishOversizedPayloadRejectsWholeBatch(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%t", partial), func(t *testing.T) {
			pool := dbtest.NewPool(t)
			proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "community_collect")
			valid := communityEnvelope(t, &proof, "valid")
			oversized := independentCommunityEnvelope(t, &proof)

			var payload contract.CommunityPayloadV1

			require.NoError(t, jsonv2.Unmarshal(oversized.Payload, &payload))

			payload.Posts = make([]contract.CommunityPostV1, 50)
			for i := range payload.Posts {
				payload.Posts[i] = contract.CommunityPostV1{PostID: fmt.Sprintf("post-%02d", i), ChannelID: testChannelID, ContentText: "x"}
			}

			raw, err := jsonv2.Marshal(payload)
			require.NoError(t, err)

			padding := contract.MaxPayloadBytes + 1 - postgresPayloadSize(t, pool, raw)

			for i := range payload.Posts {
				n := padding / len(payload.Posts)
				if i < padding%len(payload.Posts) {
					n++
				}

				payload.Posts[i].ContentText += strings.Repeat("x", n)
			}

			require.NoError(t, payload.Validate(testChannelID))

			raw, err = jsonv2.Marshal(payload)
			require.NoError(t, err)

			oversized.Payload, err = contract.CanonicalizeJSON(raw)
			require.NoError(t, err)
			require.Equal(t, contract.MaxPayloadBytes+1, postgresPayloadSize(t, pool, oversized.Payload))

			_, prepareErr := contract.PrepareEnvelope(*oversized)
			require.ErrorIs(t, prepareErr, contract.ErrPayloadTooLarge)

			input := publishInput(valid)

			input.Observations = append(input.Observations, *oversized)
			input.Checkpoint.Entries = append(input.Checkpoint.Entries, checkpointForEnvelope(oversized))

			repo := NewRepository(pool)

			if partial {
				_, err = repo.PublishBatchAndDefer(t.Context(), input, mustTestDeferInput(t, contract.ErrorResponseTooLarge, contract.ClassResourceLimit, "payload storage limit"))
			} else {
				_, err = repo.PublishBatch(t.Context(), input)
			}

			require.ErrorIs(t, err, contract.ErrPayloadTooLarge)

			assertPublishSideEffects(t, pool, 0, 0, 0)
			assertTableCount(t, pool, "source_observation_payloads", 0)

			if state := readLeaseTerminal(t.Context(), t, pool, proof.JobKey); state.state != "ACTIVE" {
				t.Fatalf("lease changed: %+v", state)
			}
		})
	}
}

func TestPublishPreservesRealCheckErrorAndRollback(t *testing.T) {
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "community_collect")
	input := publishInput(communityEnvelope(t, &proof, "check-failure"))

	if _, err := pool.Exec(t.Context(), `ALTER TABLE source_observation_payloads ADD CONSTRAINT test_publish_stream_check CHECK (false) NOT VALID`); err != nil {
		t.Fatal(err)
	}

	_, err := NewRepository(pool).PublishBatch(t.Context(), input)
	pgErr, ok := errors.AsType[*pgconn.PgError](err)

	if !ok || pgErr.Code != "23514" || pgErr.ConstraintName != "test_publish_stream_check" {
		t.Fatalf("lost CHECK failure: %v", err)
	}

	assertPublishSideEffects(t, pool, 0, 0, 0)
	assertTableCount(t, pool, "source_observation_payloads", 0)

	if state := readLeaseTerminal(t.Context(), t, pool, proof.JobKey); state.state != "ACTIVE" {
		t.Fatalf("lease changed: %+v", state)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := NewRepository(pool).PublishBatch(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}

func TestPublishStoresVerifiedCanonicalPayload(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	envelope, canonical := paddedSchedulePublication(t, pool)
	proof := envelope.Lease
	rawPayloadSHA := contract.SHA256Hex(envelope.Payload)

	require.Len(t, envelope.Payload, contract.MaxPayloadBytes)
	require.Greater(t, postgresPayloadSize(t, pool, envelope.Payload), contract.MaxPayloadBytes)

	require.NoError(t, envelope.Validate())

	repo := NewRepository(pool)
	input := publishInput(&envelope)

	first, err := repo.PublishBatch(ctx, input)
	require.NoError(t, err)

	var storedMatches bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT payload=$1::jsonb AND encode(payload_sha256,'hex')=$2
		FROM source_observation_payloads`, string(canonical), envelope.PayloadSHA256).Scan(&storedMatches))

	require.True(t, storedMatches)
	require.Equal(t, rawPayloadSHA, contract.SHA256Hex(input.Observations[0].Payload))

	reactivateLease(t, pool, &proof)

	envelope.Payload = canonical

	duplicate, err := repo.PublishBatch(ctx, publishInput(&envelope))
	require.NoError(t, err)

	require.Equal(t, PublishDuplicate, duplicate.Results[0].Outcome)
	require.Equal(t, first.Results[0].ObservationID, duplicate.Results[0].ObservationID)

	assertPublishSideEffects(t, pool, 1, 1, 1)

	next := advanceLease(ctx, t, pool, &proof)

	envelope.Lease, envelope.ScheduledFor, envelope.ObservedAt = next, next.ScheduledFor, next.ScheduledFor.Add(time.Second)

	envelope, err = contract.PrepareEnvelope(envelope)
	require.NoError(t, err)

	published, err := repo.PublishBatch(ctx, publishInput(&envelope))
	require.NoError(t, err)

	require.Equal(t, PublishInserted, published.Results[0].Outcome)

	assertPublishSideEffects(t, pool, 2, 2, 1)
	assertTableCount(t, pool, "source_observation_payloads", 1)
}

func paddedSchedulePublication(t *testing.T, pool *pgxpool.Pool) (contract.Envelope, []byte) {
	t.Helper()

	ctx := t.Context()

	proof := seedPublishLease(ctx, t, pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "official_schedule")

	const subject = "global:hololive-schedule"

	_, err := pool.Exec(ctx, `UPDATE youtube_collection_targets SET subject_key=$1 WHERE projection_generation=$2`,
		subject, proof.ProjectionGeneration)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_collection_job_leases SET subject_key=$1 WHERE job_key=$2`,
		subject, proof.JobKey)
	require.NoError(t, err)

	payload := contract.ScheduleSnapshotV1{
		GroupKey: subject, Coverage: contract.ScheduleCoverageV1{GroupKey: subject},
		Items: []contract.ScheduleItemV1{{
			ExternalID: "event-1", Title: "x", ScheduledAt: proof.ScheduledFor,
			CollaboTalentNames: []string{"x"},
		}},
	}

	raw, err := jsonv2.Marshal(payload)
	require.NoError(t, err)

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderHololiveOfficial, ObservationKind: contract.KindSchedule,
		SubjectKey: subject, SchemaVersion: contract.SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityNotApplicable,
		Payload: raw, CollectorInstance: proof.OwnerInstance, Lease: proof,
	})
	require.NoError(t, err)

	canonical := envelope.Payload

	payload.Items[0].CollaboTalentNames[0] += strings.Repeat(" ", contract.MaxPayloadBytes-len(raw))

	padded, err := jsonv2.Marshal(payload)
	require.NoError(t, err)

	envelope.Payload = padded

	return envelope, canonical
}
