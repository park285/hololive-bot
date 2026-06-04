package openapi

import "testing"

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
