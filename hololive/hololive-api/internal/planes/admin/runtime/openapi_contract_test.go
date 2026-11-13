package adminruntime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

const roomsPath = "/api/holo/rooms"

// adminOpenAPI는 admin plane /api/holo/* 생산자 계약(docs/current/contracts/admin-api.openapi.json)이다.
type adminOpenAPI struct {
	Paths      map[string]map[string]openAPIOperation `json:"paths"`
	Components struct {
		Schemas map[string]json.RawMessage `json:"schemas"`
	} `json:"components"`
}

type openAPIOperation struct {
	RequestBody struct {
		Content map[string]struct {
			Schema json.RawMessage `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
	Responses map[string]struct {
		Content map[string]struct {
			Schema json.RawMessage `json:"schema"`
		} `json:"content"`
	} `json:"responses"`
}

func loadAdminOpenAPI(t *testing.T) *adminOpenAPI {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}

	root, err := os.OpenRoot(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", "..", "docs", "current", "contracts"))
	if err != nil {
		t.Fatalf("open contracts directory: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	raw, err := root.ReadFile("admin-api.openapi.json")
	if err != nil {
		t.Fatalf("read admin OpenAPI: %v", err)
	}

	var spec adminOpenAPI

	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode admin OpenAPI: %v", err)
	}

	return &spec
}

// 해당 status 응답이 없으면 default(ProducerError)를 쓴다.
func (s *adminOpenAPI) assertResponse(t *testing.T, method, path string, status int, body []byte) {
	t.Helper()

	operation, ok := s.Paths[path][strings.ToLower(method)]
	if !ok {
		t.Fatalf("contract has no operation %s %s", method, path)
	}

	response, ok := operation.Responses[strconv.Itoa(status)]
	if !ok {
		response, ok = operation.Responses["default"]
	}

	if !ok {
		t.Fatalf("contract has no %d response for %s %s", status, method, path)
	}

	var value any

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("%s %s %d body is not JSON: %v: %s", method, path, status, err, body)
	}

	if err := s.check(response.Content["application/json"].Schema, value, "$"); err != nil {
		t.Fatalf("%s %s %d body violates contract: %v: %s", method, path, status, err, body)
	}
}

// 요청 본문이 계약의 requestBody schema를 지키는지가 생산자 입력 검증의 수락 여부와 같아야 한다.
// 생산자가 400으로 거절한 요청은 schema도 거절하고, 받아들인 요청은 schema를 지킨다.
func (s *adminOpenAPI) assertRequest(t *testing.T, method, path, body string, accepted bool) {
	t.Helper()

	schema := s.Paths[path][strings.ToLower(method)].RequestBody.Content["application/json"].Schema
	if len(schema) == 0 {
		t.Fatalf("contract has no request body for %s %s", method, path)
	}

	var value any

	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()

	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("%s %s request is not JSON: %v: %s", method, path, err, body)
	}

	err := s.check(schema, value, "$")

	switch {
	case accepted && err != nil:
		t.Fatalf("%s %s producer accepted a request the contract rejects: %v: %s", method, path, err, body)
	case !accepted && err == nil:
		t.Fatalf("%s %s producer rejected a request the contract accepts: %s", method, path, body)
	}
}

// 계약이 쓰는 JSON Schema 키워드만 담는다. 형식(format)·uniqueItems·description은 판정하지 않는다.
type jsonSchema struct {
	Ref                  string                     `json:"$ref"`
	Type                 json.RawMessage            `json:"type"`
	Properties           map[string]json.RawMessage `json:"properties"`
	Required             []string                   `json:"required"`
	AdditionalProperties json.RawMessage            `json:"additionalProperties"`
	Items                json.RawMessage            `json:"items"`
	Enum                 []any                      `json:"enum"`
	Const                any                        `json:"const"`
	Pattern              string                     `json:"pattern"`
	MinLength            *int                       `json:"minLength"`
	MaxLength            *int                       `json:"maxLength"`
	Minimum              *float64                   `json:"minimum"`
	Maximum              *float64                   `json:"maximum"`
	MinItems             *int                       `json:"minItems"`
	MaxItems             *int                       `json:"maxItems"`
}

func (s *adminOpenAPI) check(raw json.RawMessage, value any, at string) error {
	var schema jsonSchema

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	if err := decoder.Decode(&schema); err != nil {
		return fmt.Errorf("%s: decode schema: %w", at, err)
	}

	if schema.Ref != "" {
		target, ok := s.Components.Schemas[strings.TrimPrefix(schema.Ref, "#/components/schemas/")]
		if !ok {
			return fmt.Errorf("%s: unresolved %s", at, schema.Ref)
		}

		return s.check(target, value, at)
	}

	if err := checkType(schema.Type, value, at); err != nil {
		return err
	}

	if schema.Const != nil && fmt.Sprint(schema.Const) != fmt.Sprint(value) {
		return fmt.Errorf("%s: %v is not const %v", at, value, schema.Const)
	}

	if schema.Enum != nil && !slices.ContainsFunc(schema.Enum, func(item any) bool { return fmt.Sprint(item) == fmt.Sprint(value) }) {
		return fmt.Errorf("%s: %v is not in enum %v", at, value, schema.Enum)
	}

	switch typed := value.(type) {
	case string:
		return checkString(&schema, typed, at)
	case json.Number:
		return checkNumber(&schema, typed, at)
	case []any:
		return s.checkArray(&schema, typed, at)
	case map[string]any:
		return s.checkObject(&schema, typed, at)
	}

	return nil
}

func checkType(raw json.RawMessage, value any, at string) error {
	if len(raw) == 0 {
		return nil
	}

	var types []string

	if err := json.Unmarshal(raw, &types); err != nil {
		var single string

		if err := json.Unmarshal(raw, &single); err != nil {
			return fmt.Errorf("%s: decode type: %w", at, err)
		}

		types = []string{single}
	}

	actual := jsonType(value)
	if slices.Contains(types, actual) || (actual == "integer" && slices.Contains(types, "number")) {
		return nil
	}

	return fmt.Errorf("%s: type %s, want %v", at, actual, types)
}

func jsonType(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number:
		if _, err := typed.Int64(); err == nil {
			return "integer"
		}

		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}

	return fmt.Sprintf("%T", value)
}

func checkString(schema *jsonSchema, value, at string) error {
	length := utf8.RuneCountInString(value)
	if schema.MinLength != nil && length < *schema.MinLength {
		return fmt.Errorf("%s: length %d < minLength %d", at, length, *schema.MinLength)
	}

	if schema.MaxLength != nil && length > *schema.MaxLength {
		return fmt.Errorf("%s: length %d > maxLength %d", at, length, *schema.MaxLength)
	}

	if schema.Pattern != "" && !regexp.MustCompile(schema.Pattern).MatchString(value) {
		return fmt.Errorf("%s: %q does not match %s", at, value, schema.Pattern)
	}

	return nil
}

func checkNumber(schema *jsonSchema, value json.Number, at string) error {
	number, err := value.Float64()
	if err != nil || math.IsInf(number, 0) {
		return fmt.Errorf("%s: invalid number %s", at, value)
	}

	if schema.Minimum != nil && number < *schema.Minimum {
		return fmt.Errorf("%s: %s < minimum %v", at, value, *schema.Minimum)
	}

	if schema.Maximum != nil && number > *schema.Maximum {
		return fmt.Errorf("%s: %s > maximum %v", at, value, *schema.Maximum)
	}

	return nil
}

func (s *adminOpenAPI) checkArray(schema *jsonSchema, value []any, at string) error {
	if schema.MinItems != nil && len(value) < *schema.MinItems {
		return fmt.Errorf("%s: %d items < minItems %d", at, len(value), *schema.MinItems)
	}

	if schema.MaxItems != nil && len(value) > *schema.MaxItems {
		return fmt.Errorf("%s: %d items > maxItems %d", at, len(value), *schema.MaxItems)
	}

	if len(schema.Items) == 0 {
		return nil
	}

	for i, item := range value {
		if err := s.check(schema.Items, item, fmt.Sprintf("%s[%d]", at, i)); err != nil {
			return err
		}
	}

	return nil
}

func (s *adminOpenAPI) checkObject(schema *jsonSchema, value map[string]any, at string) error {
	for _, name := range schema.Required {
		if _, ok := value[name]; !ok {
			return fmt.Errorf("%s: missing required %q", at, name)
		}
	}

	for name, item := range value {
		property, ok := schema.Properties[name]
		if ok {
			if err := s.check(property, item, at+"."+name); err != nil {
				return err
			}

			continue
		}

		switch string(schema.AdditionalProperties) {
		case "", "true":
		case "false":
			return fmt.Errorf("%s: unexpected property %q", at, name)
		default:
			if err := s.check(schema.AdditionalProperties, item, at+"."+name); err != nil {
				return err
			}
		}
	}

	return nil
}

func TestRoomOperationsMatchAdminOpenAPI(t *testing.T) {
	spec := loadAdminOpenAPI(t)
	serve, _, _ := newAdminTestRouter(t, &recordingIrisRooms{})

	steps := []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, roomsPath, "", http.StatusOK},
		{http.MethodPost, roomsPath, `{"room":"3010","mode":"whitelist"}`, http.StatusOK},
		{http.MethodPost, roomsPath, `{"room":"3010","mode":"whitelist"}`, http.StatusOK},
		{http.MethodPost, roomsPath, `{"room":"3011","mode":"blacklist"}`, http.StatusConflict},
		{http.MethodPost, roomsPath, `{"room":"room name","mode":"whitelist"}`, http.StatusBadRequest},
		{http.MethodPost, roomsPath, `{"room":"3012"}`, http.StatusBadRequest},
		{http.MethodPost, roomsPath, `{"room":"3012","mode":"graylist"}`, http.StatusBadRequest},
		{http.MethodPost, roomsPath, `{"room":"3012","mode":"whitelist","note":"ignored"}`, http.StatusOK},
		{http.MethodGet, roomsPath, "", http.StatusOK},
		{http.MethodDelete, roomsPath, `{"room":"3010","mode":"whitelist"}`, http.StatusOK},
		{http.MethodDelete, roomsPath, `{"room":"3010","mode":"whitelist"}`, http.StatusOK},
		{http.MethodDelete, roomsPath, `{"room":"","mode":"whitelist"}`, http.StatusBadRequest},
		{http.MethodGet, "/api/holo/rooms/joined", "", http.StatusOK},
		{http.MethodPost, "/api/holo/rooms/acl", `{"enabled":true,"mode":"blacklist"}`, http.StatusOK},
		{http.MethodPost, "/api/holo/rooms/acl", `{"mode":"graylist"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/holo/rooms/acl", `{"enabled":"yes"}`, http.StatusBadRequest},
	}

	for _, step := range steps {
		rec := serve(step.method, step.path, step.body)
		if rec.Code != step.status {
			t.Fatalf("%s %s %s = %d %s, want %d", step.method, step.path, step.body, rec.Code, rec.Body.String(), step.status)
		}

		if step.body != "" {
			spec.assertRequest(t, step.method, step.path, step.body, rec.Code != http.StatusBadRequest)
		}

		spec.assertResponse(t, step.method, step.path, rec.Code, rec.Body.Bytes())
	}
}

// seedSchedulePosterSources는 목록 응답이 세 출처의 게시물·원본 URL을 모두 담게 한다.
func seedSchedulePosterSources(t *testing.T, pool *pgxpool.Pool, store *scheduleposter.Store) {
	t.Helper()

	_, err := store.InsertCandidates(t.Context(), []scheduleposter.Candidate{{
		ID: 2119784604752343207, ChannelID: "UCROQtXcp2loQEmvpe5rhJzQ", XUserID: "2054825907916054528",
		PostURL: "https://x.com/i/status/2119784604752343207", ImageURL: "https://pbs.twimg.com/media/a.jpg?name=orig",
		TextExcerpt: "週間スケジュール", PostedAt: time.Date(2026, time.November, 9, 3, 0, 0, 0, time.UTC),
	}}, false)
	if err != nil {
		t.Fatal(err)
	}

	const melaChannel = "UC8eitCE9Z6EwUCs-VUi1blg"

	_, err = pool.Exec(t.Context(), `INSERT INTO schedule_posters (id, source, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt)
		VALUES (7, 'youtube_community', $1, '', 'https://www.youtube.com/post/UgkxSchedule', 'https://yt3.ggpht.com/poster=s0', now(), 'スケジュール')`,
		melaChannel)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.InsertThumbnailCandidates(t.Context(), []scheduleposter.ThumbnailCandidate{{
		ChannelID: melaChannel, VideoID: "4uuYjSf0rpU", ImageSHA256: strings.Repeat("a", 64), PostedAt: time.Now(),
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSchedulePosterOperationsMatchAdminOpenAPI(t *testing.T) {
	spec := loadAdminOpenAPI(t)
	serve, _, pool := newAdminTestRouter(t, &recordingIrisRooms{})

	store, err := scheduleposter.NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}

	seedSchedulePosterSources(t, pool, store)

	const (
		base      = "/api/holo/schedule-posters"
		poster    = base + "/2119784604752343207"
		itemRoute = base + "/{id}"
	)

	steps := []struct {
		method, path, route, body string
		status                    int
	}{
		{http.MethodGet, base + "/summary", base + "/summary", "", http.StatusOK},
		{http.MethodGet, base + "?status=pending_review", base, "", http.StatusOK},
		{http.MethodGet, base + "?status=unknown", base, "", http.StatusBadRequest},
		{http.MethodGet, poster, itemRoute, "", http.StatusOK},
		{http.MethodGet, base + "/1", itemRoute, "", http.StatusNotFound},
		{http.MethodGet, poster + "/translated", itemRoute + "/translated", "", http.StatusNotFound},
		{http.MethodPost, poster + "/confirm", itemRoute + "/confirm", `{"expectedRevision":"2","weekStart":"2026-11-09","weekEnd":"2026-11-15"}`, http.StatusConflict},
		{http.MethodPost, poster + "/confirm", itemRoute + "/confirm", `{"expectedRevision":"1","weekStart":"2026-12-07","weekEnd":"2026-12-13"}`, http.StatusConflict},
		{http.MethodPost, poster + "/confirm", itemRoute + "/confirm", `{"expectedRevision":"1","weekStart":"11/9"}`, http.StatusBadRequest},
		{http.MethodPost, poster + "/week", itemRoute + "/week", `{"expectedRevision":"1","weekStart":"2026-11-09","weekEnd":"2026-11-15"}`, http.StatusConflict},
		{http.MethodPost, poster + "/confirm", itemRoute + "/confirm", `{"expectedRevision":"1","weekStart":"2026-11-09","weekEnd":"2026-11-15"}`, http.StatusOK},
		{http.MethodGet, poster, itemRoute, "", http.StatusOK},
		{http.MethodPost, poster + "/translate", itemRoute + "/translate", `{"expectedRevision":"2"}`, http.StatusConflict},
		{http.MethodPost, poster + "/week", itemRoute + "/week", `{"expectedRevision":"2","weekStart":"2026-11-23","weekEnd":"2026-11-29"}`, http.StatusConflict},
		{http.MethodPost, poster + "/week", itemRoute + "/week", `{"expectedRevision":"2","weekStart":"11/16"}`, http.StatusBadRequest},
		{http.MethodPost, poster + "/week", itemRoute + "/week", `{"expectedRevision":"2","weekStart":"2026-11-16","weekEnd":"2026-11-22"}`, http.StatusOK},
		{http.MethodPost, poster + "/reject", itemRoute + "/reject", `{"expectedRevision":"x"}`, http.StatusBadRequest},
		{http.MethodPost, poster + "/reject", itemRoute + "/reject", `{"expectedRevision":"3","note":"x"}`, http.StatusBadRequest},
		{http.MethodPost, poster + "/reject", itemRoute + "/reject", `{"expectedRevision":"3"}`, http.StatusOK},
		{http.MethodGet, base + "?status=rejected", base, "", http.StatusOK},
		{http.MethodGet, base + "/summary", base + "/summary", "", http.StatusOK},
		{http.MethodPost, base + "/collect", base + "/collect", `{"now":true}`, http.StatusBadRequest},
		{http.MethodPost, base + "/collect", base + "/collect", `{}`, http.StatusOK},
		{http.MethodPost, base + "/collect", base + "/collect", `{}`, http.StatusOK},
	}

	for _, step := range steps {
		rec := serve(step.method, step.path, step.body)
		if rec.Code != step.status {
			t.Fatalf("%s %s %s = %d %s, want %d", step.method, step.path, step.body, rec.Code, rec.Body.String(), step.status)
		}

		if step.body != "" {
			spec.assertRequest(t, step.method, step.route, step.body, rec.Code != http.StatusBadRequest)
		}

		spec.assertResponse(t, step.method, step.route, rec.Code, rec.Body.Bytes())
	}
}
