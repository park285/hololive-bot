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
	"unicode/utf8"
)

// roomsPath는 방 목록·변경 operation 경로다.
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

// 계약의 operation·status 응답 schema로 handler 응답 본문을 검사한다.
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

// room·ACL 관리 operation의 실제 handler 요청 수락 여부와 응답은 생산자 계약 schema를 지킨다. 조립은 process와 같은
// buildAdminHandler·ProvideAPIRouter 경로이고 ACL Service는 시험 DB를 쓰는 실제 인스턴스다.
func TestRoomOperationsMatchAdminOpenAPI(t *testing.T) {
	spec := loadAdminOpenAPI(t)
	serve, _ := newAdminTestRouter(t, &recordingIrisRooms{})

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
