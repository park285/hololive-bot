package sourceobservation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProviderAndObservationKindVocabulary(t *testing.T) {
	for _, provider := range []Provider{ProviderHolodex, ProviderYouTubeJS, ProviderHololiveOfficial} {
		if !provider.Valid() {
			t.Fatalf("provider %q is invalid", provider)
		}
	}
	for _, kind := range []ObservationKind{
		KindCommunityPage, KindVideoList, KindShortsList, KindLiveSnapshot,
		KindViewerSample, KindChannelStats, KindChannelProfile, KindChannelPhoto, KindSchedule,
	} {
		if !kind.Valid() {
			t.Fatalf("kind %q is invalid", kind)
		}
	}
}

func TestPrepareEnvelopeCanonicalizesCoverageAndHashes(t *testing.T) {
	envelope := newCommunityEnvelope(t, time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC))
	prepared, err := PrepareEnvelope(envelope)
	if err != nil {
		t.Fatalf("prepare envelope: %v", err)
	}
	if err := prepared.Validate(); err != nil {
		t.Fatalf("validate prepared envelope: %v", err)
	}
	for name, value := range map[string]string{
		"scope": prepared.ScopeSHA256, "payload": prepared.PayloadSHA256, "evidence": prepared.EvidenceSHA256,
	} {
		if len(value) != 64 {
			t.Fatalf("%s hash length = %d", name, len(value))
		}
	}
}

func TestSnapshotObservationIdentityPreservesNextSlot(t *testing.T) {
	first := newCommunityEnvelope(t, time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC))
	first, err := PrepareEnvelope(first)
	if err != nil {
		t.Fatalf("prepare first: %v", err)
	}
	second := newCommunityEnvelope(t, first.ScheduledFor.Add(time.Minute))
	second, err = PrepareEnvelope(second)
	if err != nil {
		t.Fatalf("prepare second: %v", err)
	}
	if first.PayloadSHA256 != second.PayloadSHA256 {
		t.Fatal("same payload changed hash")
	}
	if first.ObservationKey == second.ObservationKey {
		t.Fatal("next scheduled slot must create a new identity")
	}
}

func TestViewerSampleIdentityPreservesEqualValueInNextWindow(t *testing.T) {
	window := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	first := newViewerEnvelope(t, window, 123)
	first, err := PrepareEnvelope(first)
	if err != nil {
		t.Fatalf("prepare first: %v", err)
	}
	second := newViewerEnvelope(t, window.Add(time.Minute), 123)
	second, err = PrepareEnvelope(second)
	if err != nil {
		t.Fatalf("prepare second: %v", err)
	}
	if first.ObservationKey == second.ObservationKey {
		t.Fatal("next viewer sample window must create a new identity")
	}
}

func TestEnvelopeRejectsUnknownAndDuplicatePayloadFields(t *testing.T) {
	base := newCommunityEnvelope(t, time.Now().UTC().Truncate(time.Second))
	base.Payload = json.RawMessage(`{"channel_id":"UC_TEST","posts":[],"coverage":{"channel_id":"UC_TEST","max_results":10,"page_count":1,"exhausted":true},"unexpected":true}`)
	if _, err := PrepareEnvelope(base); err == nil {
		t.Fatal("unknown payload field must be rejected")
	}
	base.Payload = json.RawMessage(`{"channel_id":"UC_TEST","channel_id":"UC_OTHER","posts":[],"coverage":{"channel_id":"UC_TEST","max_results":10,"page_count":1,"exhausted":true}}`)
	if _, err := PrepareEnvelope(base); err == nil {
		t.Fatal("duplicate payload field must be rejected")
	}
}

func TestStrictJSONRejectsNonCanonicalFieldsAtEveryContractLevel(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "envelope",
			raw:  `{"Provider":"youtubejs"}`,
			want: "canonical field",
		},
		{
			name: "lease",
			raw:  `{"lease":{"OwnerInstance":"collector-a"}}`,
			want: "canonical field",
		},
		{
			name: "payload",
			raw:  `{"Channel_ID":"UC_TEST"}`,
			want: "canonical field",
		},
		{
			name: "nested payload",
			raw:  `{"channel_id":"UC_TEST","posts":[],"coverage":{"Channel_ID":"UC_TEST"}}`,
			want: "canonical field",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var destination any
			if tt.name == "envelope" || tt.name == "lease" {
				destination = &Envelope{}
			} else {
				destination = &CommunityPayloadV1{}
			}
			err := decodeStrictJSON([]byte(tt.raw), destination)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("decode strict JSON error = %v, want %q", err, tt.want)
			}
		})
	}

	base := newCommunityEnvelope(t, time.Now().UTC().Truncate(time.Second))
	base.Payload = json.RawMessage(`{"channel_id":"UC_TEST","Channel_ID":"UC_TEST","posts":[],"coverage":{"channel_id":"UC_TEST","max_results":10,"page_count":1,"exhausted":true}}`)
	if _, err := PrepareEnvelope(base); err == nil {
		t.Fatal("case-folded duplicate payload aliases must be rejected")
	}
}

func TestStrictJSONRejectsInvalidUnicode(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "invalid utf8", raw: []byte{'{', '"', 'v', '"', ':', '"', 0xff, '"', '}'}},
		{name: "lone high surrogate", raw: []byte(`{"v":"\uD800"}`)},
		{name: "lone low surrogate", raw: []byte(`{"v":"\uDC00"}`)},
		{name: "high surrogate followed by non-low", raw: []byte(`{"v":"\uD800\u0061"}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := CanonicalizeJSON(tt.raw); err == nil {
				t.Fatal("invalid Unicode must be rejected")
			}
		})
	}
}

func TestPaginatedPayloadCompletenessRequiresExhaustedCoverage(t *testing.T) {
	tests := []struct {
		kind    ObservationKind
		payload json.RawMessage
	}{
		{
			kind: KindCommunityPage,
			payload: mustMarshalPayload(t, CommunityPayloadV1{
				ChannelID: "UC_TEST",
				Coverage:  CommunityPageCoverageV1{ChannelID: "UC_TEST", MaxResults: 10, PageCount: 1},
			}),
		},
		{
			kind: KindVideoList,
			payload: mustMarshalPayload(t, VideoListV1{
				ChannelID: "UC_TEST",
				Coverage:  ChannelListCoverageV1{ChannelID: "UC_TEST", MaxResults: 10},
			}),
		},
		{
			kind: KindShortsList,
			payload: mustMarshalPayload(t, ShortsListV1{
				ChannelID: "UC_TEST",
				Coverage:  ShortsListCoverageV1{ChannelID: "UC_TEST", MaxResults: 10},
			}),
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			envelope := newPaginatedEnvelope(t, tt.kind, tt.payload, CompletenessComplete)
			if _, err := PrepareEnvelope(envelope); err == nil {
				t.Fatal("COMPLETE paginated payload with non-exhausted coverage must be rejected")
			}

			envelope.Completeness = CompletenessPartial
			if _, err := PrepareEnvelope(envelope); err != nil {
				t.Fatalf("PARTIAL paginated payload with non-exhausted coverage: %v", err)
			}
		})
	}
}

func TestTypedPayloadTimesCanonicalizeToUTC(t *testing.T) {
	published := time.Date(2026, 8, 14, 1, 0, 0, 0, time.FixedZone("KST", 9*60*60))
	scheduled := published.Add(time.Hour)
	videoPayload := mustMarshalPayload(t, VideoListV1{
		ChannelID: "UC_TEST",
		Videos:    []VideoListItemV1{{VideoID: "video-1", ChannelID: "UC_TEST", Title: "title", PublishedAt: &published, ScheduledFor: &scheduled}},
		Coverage: ChannelListCoverageV1{
			ChannelID: "UC_TEST", MaxResults: 10,
			Filters: VideoListFiltersV1{PublishedAfter: &published, PublishedBefore: &scheduled},
		},
	})
	prepared, err := PrepareEnvelope(newPaginatedEnvelope(t, KindVideoList, videoPayload, CompletenessPartial))
	if err != nil {
		t.Fatalf("prepare video payload: %v", err)
	}
	if strings.Contains(string(prepared.Payload), "+09:00") || !strings.Contains(string(prepared.Payload), "Z") {
		t.Fatalf("video payload times were not canonicalized to UTC: %s", prepared.Payload)
	}

	started := published.Add(2 * time.Hour)
	ended := published.Add(3 * time.Hour)
	livePayload := mustMarshalPayload(t, LiveSnapshotV1{
		Sessions: []LiveSessionV1{{VideoID: "video-1", ChannelID: "UC_TEST", Status: "ENDED", ScheduledAt: &published, StartedAt: &started, EndedAt: &ended}},
		Coverage: GlobalChannelCoverageV1{RequestedChannelIDs: []string{"UC_TEST"}, GroupKey: "UC_TEST", Filters: LiveFiltersV1{Statuses: []string{"ENDED"}}},
	})
	prepared, err = PrepareEnvelope(newPaginatedEnvelope(t, KindLiveSnapshot, livePayload, CompletenessComplete))
	if err != nil {
		t.Fatalf("prepare live payload: %v", err)
	}
	if strings.Contains(string(prepared.Payload), "+09:00") {
		t.Fatalf("live session times were not canonicalized to UTC: %s", prepared.Payload)
	}

	schedulePayload := mustMarshalPayload(t, ScheduleSnapshotV1{
		GroupKey: "UC_TEST",
		Items:    []ScheduleItemV1{{ExternalID: "schedule-1", Title: "title", ScheduledAt: published, EndedAt: &ended}},
		Coverage: ScheduleCoverageV1{GroupKey: "UC_TEST", WindowStart: &published, WindowEnd: &ended},
	})
	prepared, err = PrepareEnvelope(newPaginatedEnvelope(t, KindSchedule, schedulePayload, CompletenessComplete))
	if err != nil {
		t.Fatalf("prepare schedule payload: %v", err)
	}
	if strings.Contains(string(prepared.Payload), "+09:00") {
		t.Fatalf("schedule times were not canonicalized to UTC: %s", prepared.Payload)
	}
}

func TestTypedPayloadRejectsNonNilZeroTimes(t *testing.T) {
	zero := time.Time{}
	tests := []struct {
		name    string
		kind    ObservationKind
		payload json.RawMessage
	}{
		{
			name: "community post",
			kind: KindCommunityPage,
			payload: mustMarshalPayload(t, CommunityPayloadV1{
				ChannelID: "UC_TEST", Posts: []CommunityPostV1{{PostID: "post-1", ChannelID: "UC_TEST", PublishedAt: &zero}},
				Coverage: CommunityPageCoverageV1{ChannelID: "UC_TEST", MaxResults: 10, PageCount: 1, Exhausted: true},
			}),
		},
		{
			name: "video item",
			kind: KindVideoList,
			payload: mustMarshalPayload(t, VideoListV1{
				ChannelID: "UC_TEST", Videos: []VideoListItemV1{{VideoID: "video-1", ChannelID: "UC_TEST", PublishedAt: &zero}},
				Coverage: ChannelListCoverageV1{ChannelID: "UC_TEST", MaxResults: 10},
			}),
		},
		{
			name: "live session",
			kind: KindLiveSnapshot,
			payload: mustMarshalPayload(t, LiveSnapshotV1{
				Sessions: []LiveSessionV1{{VideoID: "video-1", ChannelID: "UC_TEST", Status: "UPCOMING", ScheduledAt: &zero}},
				Coverage: GlobalChannelCoverageV1{RequestedChannelIDs: []string{"UC_TEST"}, GroupKey: "UC_TEST", Filters: LiveFiltersV1{Statuses: []string{"UPCOMING"}}},
			}),
		},
		{
			name: "schedule item",
			kind: KindSchedule,
			payload: mustMarshalPayload(t, ScheduleSnapshotV1{
				GroupKey: "UC_TEST", Items: []ScheduleItemV1{{ExternalID: "schedule-1", Title: "title", ScheduledAt: time.Unix(1, 0), EndedAt: &zero}},
				Coverage: ScheduleCoverageV1{GroupKey: "UC_TEST"},
			}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := PrepareEnvelope(newPaginatedEnvelope(t, tt.kind, tt.payload, CompletenessPartial)); err == nil {
				t.Fatal("non-nil zero optional time must be rejected")
			}
		})
	}
}

func TestTypedCoverageBindsLiveAndMetadataEntries(t *testing.T) {
	validTests := []struct {
		name    string
		kind    ObservationKind
		payload json.RawMessage
	}{
		{
			name: "live session channel and status",
			kind: KindLiveSnapshot,
			payload: mustMarshalPayload(t, LiveSnapshotV1{
				Sessions: []LiveSessionV1{{VideoID: "video-1", ChannelID: "UC_A", Status: "LIVE"}},
				Coverage: GlobalChannelCoverageV1{
					RequestedChannelIDs: []string{"UC_A", "UC_B"}, GroupKey: "UC_TEST",
					Filters: LiveFiltersV1{Statuses: []string{"LIVE", "UPCOMING"}},
				},
			}),
		},
		{
			name: "explicit live cancellation",
			kind: KindLiveSnapshot,
			payload: mustMarshalPayload(t, LiveSnapshotV1{
				Sessions: []LiveSessionV1{{VideoID: "video-cancelled", ChannelID: "UC_A", Status: "CANCELLED"}},
				Coverage: GlobalChannelCoverageV1{
					RequestedChannelIDs: []string{"UC_A"}, GroupKey: "UC_TEST",
					Filters: LiveFiltersV1{Statuses: []string{"CANCELLED"}},
				},
			}),
		},
		{
			name: "channel stats field",
			kind: KindChannelStats,
			payload: func() json.RawMessage {
				count := int64(10)
				return mustMarshalPayload(t, ChannelStatsV1{
					ChannelID: "UC_TEST", SubscriberCount: &count,
					Coverage: ChannelStatsCoverageV1{ChannelID: "UC_TEST", Fields: []string{"subscriber_count"}},
				})
			}(),
		},
		{
			name: "channel profile field",
			kind: KindChannelProfile,
			payload: mustMarshalPayload(t, ChannelProfileV1{
				ChannelID: "UC_TEST", Handle: FieldValue[string]{Present: true, Value: "handle"},
				Coverage: ChannelProfileCoverageV1{ChannelID: "UC_TEST", Fields: []string{"handle"}},
			}),
		},
		{
			name: "channel photo variant",
			kind: KindChannelPhoto,
			payload: mustMarshalPayload(t, ChannelPhotoV1{
				ChannelID: "UC_TEST", Variants: []PhotoVariantV1{{Kind: "avatar", URL: "https://example.test/avatar.jpg"}},
				Coverage: ChannelPhotoCoverageV1{ChannelID: "UC_TEST", Variants: []string{"avatar"}},
			}),
		},
	}
	for _, tt := range validTests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := PrepareEnvelope(newPaginatedEnvelope(t, tt.kind, tt.payload, CompletenessPartial)); err != nil {
				t.Fatalf("valid in-coverage entry rejected: %v", err)
			}
		})
	}

	invalidTests := []struct {
		name    string
		kind    ObservationKind
		payload json.RawMessage
	}{
		{
			name: "live session channel outside coverage",
			kind: KindLiveSnapshot,
			payload: mustMarshalPayload(t, LiveSnapshotV1{
				Sessions: []LiveSessionV1{{VideoID: "video-1", ChannelID: "UC_B", Status: "LIVE"}},
				Coverage: GlobalChannelCoverageV1{
					RequestedChannelIDs: []string{"UC_A"}, GroupKey: "UC_TEST",
					Filters: LiveFiltersV1{Statuses: []string{"LIVE"}},
				},
			}),
		},
		{
			name: "live session status outside coverage",
			kind: KindLiveSnapshot,
			payload: mustMarshalPayload(t, LiveSnapshotV1{
				Sessions: []LiveSessionV1{{VideoID: "video-1", ChannelID: "UC_A", Status: "UPCOMING"}},
				Coverage: GlobalChannelCoverageV1{
					RequestedChannelIDs: []string{"UC_A"}, GroupKey: "UC_TEST",
					Filters: LiveFiltersV1{Statuses: []string{"LIVE"}},
				},
			}),
		},
		{
			name: "channel stats field outside coverage",
			kind: KindChannelStats,
			payload: func() json.RawMessage {
				count := int64(10)
				return mustMarshalPayload(t, ChannelStatsV1{
					ChannelID: "UC_TEST", SubscriberCount: &count,
					Coverage: ChannelStatsCoverageV1{ChannelID: "UC_TEST", Fields: []string{"view_count"}},
				})
			}(),
		},
		{
			name: "channel profile field outside coverage",
			kind: KindChannelProfile,
			payload: mustMarshalPayload(t, ChannelProfileV1{
				ChannelID: "UC_TEST", Handle: FieldValue[string]{Present: true, Value: "handle"},
				Coverage: ChannelProfileCoverageV1{ChannelID: "UC_TEST", Fields: []string{"country"}},
			}),
		},
		{
			name: "channel photo variant outside coverage",
			kind: KindChannelPhoto,
			payload: mustMarshalPayload(t, ChannelPhotoV1{
				ChannelID: "UC_TEST", Variants: []PhotoVariantV1{{Kind: "avatar", URL: "https://example.test/avatar.jpg"}},
				Coverage: ChannelPhotoCoverageV1{ChannelID: "UC_TEST", Variants: []string{"banner"}},
			}),
		},
	}
	for _, tt := range invalidTests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := PrepareEnvelope(newPaginatedEnvelope(t, tt.kind, tt.payload, CompletenessPartial)); err == nil {
				t.Fatal("out-of-coverage entry must be rejected")
			}
		})
	}
}

func TestTypedCoverageBindsItemTimes(t *testing.T) {
	windowStart := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(time.Hour)
	tests := []struct {
		name    string
		kind    ObservationKind
		payload json.RawMessage
	}{
		{
			name: "video before coverage",
			kind: KindVideoList,
			payload: mustMarshalPayload(t, VideoListV1{
				ChannelID: "UC_TEST",
				Videos: []VideoListItemV1{{
					VideoID: "video-1", ChannelID: "UC_TEST", PublishedAt: new(windowStart.Add(-time.Second)),
				}},
				Coverage: ChannelListCoverageV1{
					ChannelID: "UC_TEST", MaxResults: 10,
					Filters: VideoListFiltersV1{PublishedAfter: &windowStart, PublishedBefore: &windowEnd},
				},
			}),
		},
		{
			name: "video after coverage",
			kind: KindVideoList,
			payload: mustMarshalPayload(t, VideoListV1{
				ChannelID: "UC_TEST",
				Videos: []VideoListItemV1{{
					VideoID: "video-1", ChannelID: "UC_TEST", PublishedAt: new(windowEnd.Add(time.Second)),
				}},
				Coverage: ChannelListCoverageV1{
					ChannelID: "UC_TEST", MaxResults: 10,
					Filters: VideoListFiltersV1{PublishedAfter: &windowStart, PublishedBefore: &windowEnd},
				},
			}),
		},
		{
			name: "schedule before coverage",
			kind: KindSchedule,
			payload: mustMarshalPayload(t, ScheduleSnapshotV1{
				GroupKey: "UC_TEST",
				Items: []ScheduleItemV1{{
					ExternalID: "schedule-1", Title: "title", ScheduledAt: windowStart.Add(-time.Second),
				}},
				Coverage: ScheduleCoverageV1{GroupKey: "UC_TEST", WindowStart: &windowStart, WindowEnd: &windowEnd},
			}),
		},
		{
			name: "schedule after coverage",
			kind: KindSchedule,
			payload: mustMarshalPayload(t, ScheduleSnapshotV1{
				GroupKey: "UC_TEST",
				Items: []ScheduleItemV1{{
					ExternalID: "schedule-1", Title: "title", ScheduledAt: windowEnd.Add(time.Second),
				}},
				Coverage: ScheduleCoverageV1{GroupKey: "UC_TEST", WindowStart: &windowStart, WindowEnd: &windowEnd},
			}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PrepareEnvelope(newPaginatedEnvelope(t, tt.kind, tt.payload, CompletenessPartial))
			if err == nil || !strings.Contains(err.Error(), "outside coverage") {
				t.Fatalf("error = %v, want outside coverage", err)
			}
		})
	}

	for _, tt := range []struct {
		name    string
		kind    ObservationKind
		payload any
	}{
		{
			name: "video boundaries",
			kind: KindVideoList,
			payload: VideoListV1{
				ChannelID: "UC_TEST",
				Videos: []VideoListItemV1{
					{VideoID: "video-start", ChannelID: "UC_TEST", PublishedAt: &windowStart},
					{VideoID: "video-end", ChannelID: "UC_TEST", PublishedAt: &windowEnd},
				},
				Coverage: ChannelListCoverageV1{
					ChannelID: "UC_TEST", MaxResults: 10,
					Filters: VideoListFiltersV1{PublishedAfter: &windowStart, PublishedBefore: &windowEnd},
				},
			},
		},
		{
			name: "schedule boundaries",
			kind: KindSchedule,
			payload: ScheduleSnapshotV1{
				GroupKey: "UC_TEST",
				Items: []ScheduleItemV1{
					{ExternalID: "schedule-start", Title: "title", ScheduledAt: windowStart},
					{ExternalID: "schedule-end", Title: "title", ScheduledAt: windowEnd},
				},
				Coverage: ScheduleCoverageV1{GroupKey: "UC_TEST", WindowStart: &windowStart, WindowEnd: &windowEnd},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			payload := mustMarshalPayload(t, tt.payload)
			if _, err := PrepareEnvelope(newPaginatedEnvelope(t, tt.kind, payload, CompletenessPartial)); err != nil {
				t.Fatalf("boundary item rejected: %v", err)
			}
		})
	}
}

func TestRequiredCollectionFieldsCanonicalizeMissingAndNullToEmpty(t *testing.T) {
	tests := []struct {
		name       string
		kind       ObservationKind
		missing    string
		nullValue  string
		emptyValue string
	}{
		{
			name:       "community posts",
			kind:       KindCommunityPage,
			missing:    `{"channel_id":"UC_TEST","coverage":{"channel_id":"UC_TEST","max_results":10,"page_count":1,"exhausted":true}}`,
			nullValue:  `{"channel_id":"UC_TEST","posts":null,"coverage":{"channel_id":"UC_TEST","max_results":10,"page_count":1,"exhausted":true}}`,
			emptyValue: `{"channel_id":"UC_TEST","posts":[],"coverage":{"channel_id":"UC_TEST","max_results":10,"page_count":1,"exhausted":true}}`,
		},
		{
			name:       "video list",
			kind:       KindVideoList,
			missing:    `{"channel_id":"UC_TEST","coverage":{"channel_id":"UC_TEST","max_results":10,"exhausted":false,"filters":{"include_upcoming":false}}}`,
			nullValue:  `{"channel_id":"UC_TEST","videos":null,"coverage":{"channel_id":"UC_TEST","max_results":10,"exhausted":false,"filters":{"include_upcoming":false}}}`,
			emptyValue: `{"channel_id":"UC_TEST","videos":[],"coverage":{"channel_id":"UC_TEST","max_results":10,"exhausted":false,"filters":{"include_upcoming":false}}}`,
		},
		{
			name:       "shorts list",
			kind:       KindShortsList,
			missing:    `{"channel_id":"UC_TEST","coverage":{"channel_id":"UC_TEST","max_results":10,"exhausted":false}}`,
			nullValue:  `{"channel_id":"UC_TEST","videos":null,"coverage":{"channel_id":"UC_TEST","max_results":10,"exhausted":false}}`,
			emptyValue: `{"channel_id":"UC_TEST","videos":[],"coverage":{"channel_id":"UC_TEST","max_results":10,"exhausted":false}}`,
		},
		{
			name:       "live sessions",
			kind:       KindLiveSnapshot,
			missing:    `{"coverage":{"requested_channel_ids":["UC_TEST"],"group_key":"UC_TEST","filters":{"statuses":["LIVE"]}}}`,
			nullValue:  `{"sessions":null,"coverage":{"requested_channel_ids":["UC_TEST"],"group_key":"UC_TEST","filters":{"statuses":["LIVE"]}}}`,
			emptyValue: `{"sessions":[],"coverage":{"requested_channel_ids":["UC_TEST"],"group_key":"UC_TEST","filters":{"statuses":["LIVE"]}}}`,
		},
		{
			name:       "photo variants",
			kind:       KindChannelPhoto,
			missing:    `{"channel_id":"UC_TEST","coverage":{"channel_id":"UC_TEST","variants":["avatar"]}}`,
			nullValue:  `{"channel_id":"UC_TEST","variants":null,"coverage":{"channel_id":"UC_TEST","variants":["avatar"]}}`,
			emptyValue: `{"channel_id":"UC_TEST","variants":[],"coverage":{"channel_id":"UC_TEST","variants":["avatar"]}}`,
		},
		{
			name:       "schedule items",
			kind:       KindSchedule,
			missing:    `{"group_key":"UC_TEST","coverage":{"group_key":"UC_TEST"}}`,
			nullValue:  `{"group_key":"UC_TEST","items":null,"coverage":{"group_key":"UC_TEST"}}`,
			emptyValue: `{"group_key":"UC_TEST","items":[],"coverage":{"group_key":"UC_TEST"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var canonicalPayload string
			var payloadSHA256 string
			for i, raw := range []string{tt.missing, tt.nullValue, tt.emptyValue} {
				prepared, err := PrepareEnvelope(newPaginatedEnvelope(t, tt.kind, json.RawMessage(raw), CompletenessPartial))
				if err != nil {
					t.Fatalf("variant %d: %v", i, err)
				}
				if i == 0 {
					canonicalPayload = string(prepared.Payload)
					payloadSHA256 = prepared.PayloadSHA256
					continue
				}
				if string(prepared.Payload) != canonicalPayload || prepared.PayloadSHA256 != payloadSHA256 {
					t.Fatalf("variant %d did not canonicalize: payload=%s hash=%s", i, prepared.Payload, prepared.PayloadSHA256)
				}
			}
		})
	}
}

func TestChannelPhotoCanonicalOrderingIncludesAllVariantFields(t *testing.T) {
	fingerprintA := strings.Repeat("a", 64)
	fingerprintB := strings.Repeat("b", 64)
	firstPayload := mustMarshalPayload(t, ChannelPhotoV1{
		ChannelID: "UC_TEST",
		Variants: []PhotoVariantV1{
			{Kind: "avatar", URL: "https://example.test/avatar.jpg", Width: 200, Height: 100, StableMediaID: "stable", ContentFingerprint: fingerprintB},
			{Kind: "avatar", URL: "https://example.test/avatar.jpg", Width: 100, Height: 100, StableMediaID: "stable", ContentFingerprint: fingerprintA},
		},
		Coverage: ChannelPhotoCoverageV1{ChannelID: "UC_TEST", Variants: []string{"avatar"}},
	})
	secondPayload := mustMarshalPayload(t, ChannelPhotoV1{
		ChannelID: "UC_TEST",
		Variants: []PhotoVariantV1{
			{Kind: "avatar", URL: "https://example.test/avatar.jpg", Width: 100, Height: 100, StableMediaID: "stable", ContentFingerprint: fingerprintA},
			{Kind: "avatar", URL: "https://example.test/avatar.jpg", Width: 200, Height: 100, StableMediaID: "stable", ContentFingerprint: fingerprintB},
		},
		Coverage: ChannelPhotoCoverageV1{ChannelID: "UC_TEST", Variants: []string{"avatar"}},
	})
	first, err := PrepareEnvelope(newPaginatedEnvelope(t, KindChannelPhoto, firstPayload, CompletenessPartial))
	if err != nil {
		t.Fatalf("prepare first photo payload: %v", err)
	}
	second, err := PrepareEnvelope(newPaginatedEnvelope(t, KindChannelPhoto, secondPayload, CompletenessPartial))
	if err != nil {
		t.Fatalf("prepare second photo payload: %v", err)
	}
	if first.PayloadSHA256 != second.PayloadSHA256 || string(first.Payload) != string(second.Payload) {
		t.Fatalf("photo permutations produced different canonical payloads: %s != %s", first.Payload, second.Payload)
	}
}

func TestEnvelopeSemanticMetadataChangesEvidenceHash(t *testing.T) {
	base, err := PrepareEnvelope(newCommunityEnvelope(t, time.Now().UTC().Truncate(time.Second)))
	if err != nil {
		t.Fatalf("prepare base: %v", err)
	}
	changed := base
	changed.Completeness = CompletenessPartial
	changed.EvidenceSHA256, err = changed.expectedEvidenceSHA256()
	if err != nil {
		t.Fatalf("hash changed: %v", err)
	}
	if base.EvidenceSHA256 == changed.EvidenceSHA256 {
		t.Fatal("completeness change must alter semantic evidence")
	}
}

func TestEffectiveAtFallsBackForFutureSourceEvent(t *testing.T) {
	receivedAt := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	scheduledFor := receivedAt.Add(-time.Minute)
	future := receivedAt.Add(DefaultMaxSourceEventFutureSkew + time.Second)
	effectiveAt, fallback := EffectiveAt(ObservationClock{
		ObservationKind: KindLiveSnapshot,
		ScheduledFor:    scheduledFor,
		SourceEventAt:   &future,
		ReceivedAt:      receivedAt,
	}, DefaultMaxSourceEventFutureSkew)
	if !fallback || !effectiveAt.Equal(scheduledFor) {
		t.Fatalf("effective=%s fallback=%v, want scheduled slot fallback", effectiveAt, fallback)
	}
}

func TestCanonicalizeJSONRejectsOversizedPayload(t *testing.T) {
	if _, err := CanonicalizeJSON([]byte(`{"value":"` + strings.Repeat("x", MaxPayloadBytes) + `"}`)); err == nil {
		t.Fatal("oversized payload must be rejected")
	}
}

func newCommunityEnvelope(t *testing.T, scheduledFor time.Time) Envelope {
	t.Helper()
	payload, err := MarshalPayloadV1(CommunityPayloadV1{
		ChannelID: "UC_TEST",
		Posts:     []CommunityPostV1{{PostID: "post-1", ChannelID: "UC_TEST"}},
		Coverage:  CommunityPageCoverageV1{ChannelID: "UC_TEST", MaxResults: 10, PageCount: 1, Exhausted: true},
	})
	if err != nil {
		t.Fatalf("marshal community payload: %v", err)
	}
	return Envelope{
		Provider: ProviderYouTubeJS, ObservationKind: KindCommunityPage,
		SubjectKey: "UC_TEST", SchemaVersion: SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: scheduledFor, ObservedAt: scheduledFor.Add(time.Second),
		Completeness: CompletenessComplete, Continuity: ContinuityContiguous,
		Payload: payload, CollectorInstance: "collector-a",
		Lease: LeaseProof{JobKey: "collector:youtubejs:community:UC_TEST", CollectionJobKind: "community_collect",
			OwnerInstance: "collector-a", FenceEpoch: 1, ProjectionGeneration: 1, ScheduledFor: scheduledFor},
	}
}

func newViewerEnvelope(t *testing.T, window time.Time, count int64) Envelope {
	t.Helper()
	payload, err := MarshalPayloadV1(ViewerSampleV1{
		VideoID: "video-1", ViewerCount: &count, Availability: "AVAILABLE",
		SampleWindowStart: window, SampleWindowSeconds: 60,
		Coverage: ViewerSampleCoverageV1{VideoID: "video-1", SampleWindowStart: window, SampleWindowSeconds: 60},
	})
	if err != nil {
		t.Fatalf("marshal viewer payload: %v", err)
	}
	return Envelope{
		Provider: ProviderHolodex, ObservationKind: KindViewerSample,
		SubjectKey: "video-1", SchemaVersion: SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: window, ObservedAt: window.Add(time.Second),
		Completeness: CompletenessComplete, Continuity: ContinuityNotApplicable,
		Payload: payload, CollectorInstance: "collector-a",
		Lease: LeaseProof{JobKey: "collector:holodex:holodex_live:global", CollectionJobKind: "holodex_live",
			OwnerInstance: "collector-a", FenceEpoch: 1, ProjectionGeneration: 1, ScheduledFor: window},
	}
}

func mustMarshalPayload(t *testing.T, value any) json.RawMessage {
	t.Helper()
	payload, err := MarshalPayloadV1(value)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return payload
}

func newPaginatedEnvelope(t *testing.T, kind ObservationKind, payload json.RawMessage, completeness Completeness) Envelope {
	t.Helper()
	scheduledFor := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	return Envelope{
		Provider: ProviderYouTubeJS, ObservationKind: kind,
		SubjectKey: "UC_TEST", SchemaVersion: SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: scheduledFor, ObservedAt: scheduledFor.Add(time.Second),
		Completeness: completeness, Continuity: ContinuityContiguous,
		Payload: payload, CollectorInstance: "collector-a",
		Lease: LeaseProof{JobKey: "collector:youtubejs:paginated:UC_TEST", CollectionJobKind: "paginated_collect",
			OwnerInstance: "collector-a", FenceEpoch: 1, ProjectionGeneration: 1, ScheduledFor: scheduledFor},
	}
}
