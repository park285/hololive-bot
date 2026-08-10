package openapi

import (
	"bytes"
	"testing"
)

func TestSpecPreservesContract(t *testing.T) {
	spec := Spec("9.9.9-test")

	info, ok := spec["info"].(map[string]any)
	if !ok || info["version"] != "9.9.9-test" {
		t.Fatalf("expected injected version, got %v", spec["info"])
	}

	paths, ok := spec["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		t.Fatalf("expected non-empty paths, got %v", spec["paths"])
	}

	expected := map[string]string{
		"/admin/api/auth/login":            "handle_login",
		"/admin/api/holo/settings":         "holoGetSettings",
		"/admin/api/holo/streams/live":     "holoGetLiveStreams",
		"/admin/api/holo/streams/upcoming": "holoGetUpcomingStreams",
	}
	for path, wantOp := range expected {
		entry, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("missing path %s", path)
		}
		found := false
		for _, op := range entry {
			if body, ok := op.(map[string]any); ok && body["operationId"] == wantOp {
				found = true
			}
		}
		if !found {
			t.Fatalf("path %s missing operationId %s", path, wantOp)
		}
	}
}

func TestMarshalSpecIsStable(t *testing.T) {
	first, err := MarshalSpec("9.9.9-test")
	if err != nil {
		t.Fatalf("marshal first spec: %v", err)
	}

	for i := range 5 {
		next, err := MarshalSpec("9.9.9-test")
		if err != nil {
			t.Fatalf("marshal spec iteration %d: %v", i, err)
		}
		if !bytes.Equal(first, next) {
			t.Fatalf("marshal spec output changed on iteration %d", i)
		}
	}
}

func TestAuthResponseSchemasMatchHandlers(t *testing.T) {
	spec := Spec("test")
	components := requireStringMap(t, spec["components"], "components")
	schemas := requireStringMap(t, components["schemas"], "components.schemas")
	session := requireStringMap(t, schemas["SessionStatusResponse"], "components.schemas.SessionStatusResponse")
	required := stringSet(requireAnySlice(t, session["required"], "SessionStatusResponse.required"))
	if !required["csrf_token"] {
		t.Fatal("SessionStatusResponse must require csrf_token")
	}
	properties := requireStringMap(t, session["properties"], "SessionStatusResponse.properties")
	if _, ok := properties["csrf_token"]; !ok {
		t.Fatal("SessionStatusResponse is missing csrf_token")
	}

	paths := requireStringMap(t, spec["paths"], "paths")
	logoutPath := requireStringMap(t, paths["/admin/api/auth/logout"], "/admin/api/auth/logout")
	logout := requireStringMap(t, logoutPath["post"], "/admin/api/auth/logout.post")
	responses := requireStringMap(t, logout["responses"], "logout.responses")
	okResponse := requireStringMap(t, responses["200"], "logout.responses.200")
	content := requireStringMap(t, okResponse["content"], "logout.responses.200.content")
	jsonContent := requireStringMap(t, content["application/json"], "logout.responses.200.content.application/json")
	schema := requireStringMap(t, jsonContent["schema"], "logout.responses.200.content.application/json.schema")
	if schema["$ref"] != "#/components/schemas/StatusOnlyResponse" {
		t.Fatalf("logout response schema = %v", schema)
	}
}

func TestAggregatedStatusIncludesSampleTimestamp(t *testing.T) {
	spec := Spec("test")
	components := requireStringMap(t, spec["components"], "components")
	schemas := requireStringMap(t, components["schemas"], "components.schemas")
	status := requireStringMap(t, schemas["AggregatedStatus"], "components.schemas.AggregatedStatus")
	required := stringSet(requireAnySlice(t, status["required"], "AggregatedStatus.required"))
	if !required["sampled_at"] {
		t.Fatal("AggregatedStatus must require sampled_at")
	}
	properties := requireStringMap(t, status["properties"], "AggregatedStatus.properties")
	if _, ok := properties["sampled_at"]; !ok {
		t.Fatal("AggregatedStatus is missing sampled_at")
	}
}

func requireStringMap(t *testing.T, value any, field string) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want map[string]any", field, value)
	}
	return result
}

func requireAnySlice(t *testing.T, value any, field string) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("%s = %T, want []any", field, value)
	}
	return result
}

func stringSet(values []any) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			set[text] = true
		}
	}
	return set
}
