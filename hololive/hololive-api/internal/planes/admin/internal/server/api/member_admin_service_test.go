package api

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valkey-io/valkey-go"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

func newMemberBoundaryHarness(t *testing.T) (*MemberHandler, *pgxpool.Pool, *miniredis.Miniredis, *recordingActivity) {
	t.Helper()

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	repo := member.NewMemberRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)
	mini := miniredis.RunT(t)
	// Miniredis의 Pub/Sub는 RESP2 제약을 따르므로 valkey-go의 별도 구독 연결을 사용한다.
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{mini.Addr()}, DisableCache: true, AlwaysRESP2: true})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(client.Close)

	cache, err := member.NewMemberCache(t.Context(), repo, &cachemocks.Client{GetClientFunc: func() valkey.Client { return client }}, logger, member.CacheConfig{EpochReconcileInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(cache.Close)

	audit := &recordingActivity{}

	service := NewMemberAdminService(repo, cache, audit, logger)
	// 멤버 경계는 ACL·Iris·LLM 의존성 없이 실제 DB와 캐시만으로 구성한다.
	return NewMemberHandler(repo, service, logger), pool, mini, audit
}

func createMemberHTTP(t *testing.T, h *MemberHandler, request CreateMemberRequest, wantStatus int) MemberMutationResult {
	t.Helper()

	body, err := jsonv2.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}

	ctx, recorder := newAPITestContext(http.MethodPost, "/api/holo/members", body)
	h.AddMember(ctx)

	if recorder.Code != wantStatus {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, wantStatus, recorder.Body.String())
	}

	var response struct {
		MemberMutationResult

		Status  string `json:"status"`
		Message string `json:"message"`
	}

	if err := jsonv2.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}

	if wantStatus == http.StatusCreated {
		if response.Status != "ok" || !response.Persisted || response.MemberID <= 0 {
			t.Fatalf("missing committed result: %+v", response)
		}
	}

	return response.MemberMutationResult
}

// recordingActivity는 member 경계가 남긴 activity 기록 수를 센다.
type recordingActivity struct {
	mu      sync.Mutex
	entries []map[string]any
}

func (a *recordingActivity) Log(_, _ string, details map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.entries = append(a.entries, details)
}

func assertMemberAuditCount(t *testing.T, audit *recordingActivity, want int) {
	t.Helper()

	audit.mu.Lock()
	defer audit.mu.Unlock()

	if len(audit.entries) != want {
		t.Fatalf("activity entries=%d want=%d: %v", len(audit.entries), want, audit.entries)
	}

	for _, details := range audit.entries {
		if details["persisted"] != true {
			t.Fatalf("activity lacks committed evidence: %+v", details)
		}
	}
}

func TestMemberCreateCacheFailureAndSameRequestRecovery(t *testing.T) {
	h, pool, mini, audit := newMemberBoundaryHarness(t)
	request := CreateMemberRequest{Name: "  Cache Recovery Member  ", ChannelID: " channel-recovery ", NameJa: " 名前 ", NameKo: " 이름 ", ShortKoreanName: " 짧은이름 ", Org: " Test Org ", Units: []string{"One", "Two"}, OfficialURL: "https://example.com/member", ChzzkChannelID: " chzzk ", TwitchUserID: " twitch ", Aliases: &domain.Aliases{Ko: []string{"별명"}, Ja: []string{"別名"}}, Birthday: new(time.Date(2000, time.February, 29, 0, 0, 0, 0, time.UTC)), DebutDate: new(time.Date(2020, time.January, 2, 0, 0, 0, 0, time.UTC))}

	mini.SetError("ERR injected member cache failure")

	first := createMemberHTTP(t, h, request, http.StatusCreated)
	if first.CacheSynced || first.Replayed {
		t.Fatalf("first result=%+v", first)
	}

	assertMemberAuditCount(t, audit, 1)
	mini.SetError("")

	second := createMemberHTTP(t, h, request, http.StatusCreated)
	if !second.CacheSynced || !second.Replayed || second.MemberID != first.MemberID {
		t.Fatalf("recovery=%+v first=%+v", second, first)
	}

	var count int

	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM members WHERE slug=$1 AND english_name=$1 AND channel_id=$2 AND japanese_name=$3 AND korean_name=$4", "Cache Recovery Member", "channel-recovery", "名前", "이름").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("canonical rows=%d", count)
	}

	assertMemberAuditCount(t, audit, 1)
}

func TestMemberCreateRejectsBlankAndStorageWidths(t *testing.T) {
	h, pool, _, audit := newMemberBoundaryHarness(t)

	for _, raw := range []string{`{}`, `{"name":" \t "}`} {
		ctx, recorder := newAPITestContext(http.MethodPost, "/api/holo/members", []byte(raw))
		h.AddMember(ctx)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("blank request status=%d", recorder.Code)
		}
	}

	fields := []struct {
		name  string
		width int
		set   func(*CreateMemberRequest, string)
	}{
		{"name", 100, func(r *CreateMemberRequest, v string) { r.Name = v }},
		{"channel", 64, func(r *CreateMemberRequest, v string) { r.ChannelID = v }},
		{"japanese", 200, func(r *CreateMemberRequest, v string) { r.NameJa = v }},
		{"korean", 200, func(r *CreateMemberRequest, v string) { r.NameKo = v }},
		{"short korean", 64, func(r *CreateMemberRequest, v string) { r.ShortKoreanName = v }},
		{"org", 50, func(r *CreateMemberRequest, v string) { r.Org = v }},
		{"chzzk", 32, func(r *CreateMemberRequest, v string) { r.ChzzkChannelID = v }},
		{"twitch", 50, func(r *CreateMemberRequest, v string) { r.TwitchUserID = v }},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			request := CreateMemberRequest{Name: "Width " + field.name}
			field.set(&request, strings.Repeat("가", field.width+1))
			createMemberHTTP(t, h, request, http.StatusBadRequest)
		})
	}

	var blanks int

	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM members WHERE slug='' OR slug LIKE 'Width %'").Scan(&blanks); err != nil {
		t.Fatal(err)
	}

	if blanks != 0 {
		t.Fatalf("invalid requests persisted %d rows", blanks)
	}

	assertMemberAuditCount(t, audit, 0)

	request := CreateMemberRequest{}

	for _, field := range fields {
		field.set(&request, strings.Repeat("가", field.width))
	}

	result := createMemberHTTP(t, h, request, http.StatusCreated)
	if !result.CacheSynced {
		t.Fatal("maximum rune widths did not synchronize")
	}
}

func TestMemberCreateConflictsOnEveryPersistedField(t *testing.T) {
	const differentMemberValue = "different"

	h, _, _, audit := newMemberBoundaryHarness(t)
	base := CreateMemberRequest{Name: "Complete Match Member"}
	createMemberHTTP(t, h, base, http.StatusCreated)

	changes := []struct {
		name   string
		change func(*CreateMemberRequest)
	}{
		{"channel", func(r *CreateMemberRequest) { r.ChannelID = differentMemberValue }},
		{"japanese", func(r *CreateMemberRequest) { r.NameJa = differentMemberValue }},
		{"korean", func(r *CreateMemberRequest) { r.NameKo = differentMemberValue }},
		{"short korean", func(r *CreateMemberRequest) { r.ShortKoreanName = differentMemberValue }},
		{"graduated", func(r *CreateMemberRequest) { r.IsGraduated = true }},
		{"aliases ko", func(r *CreateMemberRequest) { r.Aliases = &domain.Aliases{Ko: []string{differentMemberValue}} }},
		{"aliases ja", func(r *CreateMemberRequest) { r.Aliases = &domain.Aliases{Ja: []string{differentMemberValue}} }},
		{"org", func(r *CreateMemberRequest) { r.Org = differentMemberValue }},
		{"units", func(r *CreateMemberRequest) { r.Units = []string{differentMemberValue} }},
		{"official link", func(r *CreateMemberRequest) { r.OfficialURL = "https://example.com/different" }},
		{"birthday", func(r *CreateMemberRequest) { r.Birthday = new(time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)) }},
		{"debut", func(r *CreateMemberRequest) {
			r.DebutDate = new(time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC))
		}},
		{"chzzk", func(r *CreateMemberRequest) { r.ChzzkChannelID = differentMemberValue }},
		{"twitch", func(r *CreateMemberRequest) { r.TwitchUserID = differentMemberValue }},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			request := base
			change.change(&request)
			createMemberHTTP(t, h, request, http.StatusConflict)
		})
	}

	assertMemberAuditCount(t, audit, 1)
}

func TestMemberCreateConcurrentIdenticalRequestsHaveOneRowAndActivity(t *testing.T) {
	h, pool, _, audit := newMemberBoundaryHarness(t)

	const calls = 6

	results := make([]MemberMutationResult, calls)
	errs := make([]error, calls)

	var wg sync.WaitGroup

	start := make(chan struct{})

	for i := range calls {
		wg.Go(func() {
			<-start

			// AddMember와 같은 순서로 요청을 검증·변환한 뒤 저장한다. create가 m.ID를 채우므로 호출마다 새 값을 만든다.
			m, err := CreateMemberRequest{Name: "Concurrent Member"}.member()
			if err != nil {
				errs[i] = err

				return
			}

			results[i], errs[i] = h.service.create(t.Context(), m)
		})
	}

	close(start)
	wg.Wait()

	created := 0

	for i, result := range results {
		if errs[i] != nil || !result.Persisted || result.MemberID != results[0].MemberID {
			t.Fatalf("result %d=%+v err=%v", i, result, errs[i])
		}

		if !result.Replayed {
			created++
		}
	}

	if created != 1 {
		t.Fatalf("new creations=%d", created)
	}

	var count int

	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM members WHERE slug='Concurrent Member'").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("rows=%d", count)
	}

	// 겹친 epoch 갱신은 다른 요청의 warmup을 무효화할 수 있다. 저장 증거는 유지하고,
	// 경합이 끝난 동일 요청이 새 행 없이 동기화를 복구해야 한다.
	recovered := createMemberHTTP(t, h, CreateMemberRequest{Name: "Concurrent Member"}, http.StatusCreated)
	if !recovered.CacheSynced || !recovered.Replayed || recovered.MemberID != results[0].MemberID {
		t.Fatalf("concurrent create recovery=%+v", recovered)
	}

	assertMemberAuditCount(t, audit, 1)
}

func TestMemberMutationsKeepCommittedResponseAndActivityOnCacheFailure(t *testing.T) {
	h, pool, mini, audit := newMemberBoundaryHarness(t)
	created := createMemberHTTP(t, h, CreateMemberRequest{Name: "Mutation Member"}, http.StatusCreated)

	mini.SetError("ERR injected mutation cache failure")

	for index, tc := range []struct {
		name, body, query string
		call              gin.HandlerFunc
	}{
		{"graduation", `{"isGraduated":true}`, "SELECT is_graduated FROM members WHERE id=$1", h.SetGraduation},
		{"channel", `{"channelId":" updated-channel "}`, "SELECT channel_id='updated-channel' FROM members WHERE id=$1", h.UpdateChannelID},
		{"name", `{"name":" Updated Member "}`, "SELECT english_name='Updated Member' FROM members WHERE id=$1", h.UpdateMemberName},
		{"alias add", `{"type":"ko","alias":"별명"}`, "SELECT aliases->'ko' ? '별명' FROM members WHERE id=$1", h.AddAlias},
		{"alias remove", `{"type":"ko","alias":"별명"}`, "SELECT NOT (aliases->'ko' ? '별명') FROM members WHERE id=$1", h.RemoveAlias},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, recorder := newAPITestContext(http.MethodPatch, "/api/holo/members/1", []byte(tc.body))

			ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprint(created.MemberID)}}
			tc.call(ctx)

			var result MemberMutationResult

			if err := jsonv2.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}

			if recorder.Code != http.StatusOK || !result.Persisted || result.CacheSynced || result.MemberID != created.MemberID {
				t.Fatalf("status=%d result=%+v body=%s", recorder.Code, result, recorder.Body.String())
			}

			var committed bool

			if err := pool.QueryRow(t.Context(), tc.query, created.MemberID).Scan(&committed); err != nil {
				t.Fatal(err)
			}

			if !committed {
				t.Fatal("mutation not committed")
			}

			assertMemberAuditCount(t, audit, index+2)
		})
	}
}

func TestMemberCreateConcurrentDifferentRequestsConflict(t *testing.T) {
	h, pool, _, audit := newMemberBoundaryHarness(t)
	requests := []CreateMemberRequest{
		{Name: "Concurrent Conflict", ChannelID: "first-channel"},
		{Name: "Concurrent Conflict", ChannelID: "second-channel"},
	}
	statuses := make([]int, len(requests))

	var wg sync.WaitGroup

	start := make(chan struct{})

	for i, request := range requests {
		body, err := jsonv2.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}

		wg.Go(func() {
			ctx, recorder := newAPITestContext(http.MethodPost, "/api/holo/members", body)

			<-start
			h.AddMember(ctx)

			statuses[i] = recorder.Code
		})
	}

	close(start)
	wg.Wait()

	slices.Sort(statuses)

	if statuses[0] != http.StatusCreated || statuses[1] != http.StatusConflict {
		t.Fatalf("concurrent statuses=%v", statuses)
	}

	var count int

	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM members WHERE slug='Concurrent Conflict'").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("conflicting requests persisted %d rows", count)
	}

	assertMemberAuditCount(t, audit, 1)
}
