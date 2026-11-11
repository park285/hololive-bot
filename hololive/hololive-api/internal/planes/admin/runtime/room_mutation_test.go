package adminruntime

import (
	"encoding/json"
	"net/http"
	"testing"
)

type roomMutationBody struct {
	Status  string `json:"status"`
	Changed *bool  `json:"changed"`
	Code    string `json:"code"`
}

func decodeRoomMutation(t *testing.T, body []byte) roomMutationBody {
	t.Helper()

	var decoded roomMutationBody

	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}

	return decoded
}

const roomChangeBody = `{"room":"3002","mode":"whitelist"}`

func assertRoomChange(t *testing.T, serve adminTestServe, method string, wantChanged bool) {
	t.Helper()

	body := roomChangeBody

	rec := serve(method, roomsPath, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s = %d %s, want 200", method, body, rec.Code, rec.Body.String())
	}

	decoded := decodeRoomMutation(t, rec.Body.Bytes())
	if decoded.Status != "ok" || decoded.Changed == nil || *decoded.Changed != wantChanged {
		t.Fatalf("%s %s body = %s, want status ok changed=%t", method, body, rec.Body.String(), wantChanged)
	}
}

func TestRoomChangesConvergeWithChangedFlag(t *testing.T) {
	serve, aclService := newAdminTestRouter(t, &recordingIrisRooms{})

	assertRoomChange(t, serve, http.MethodPost, true)
	assertRoomChange(t, serve, http.MethodPost, false)

	if !aclService.IsRoomAllowed("3002") {
		t.Fatal("repeated add left the room out of the whitelist")
	}

	assertRoomChange(t, serve, http.MethodDelete, true)
	assertRoomChange(t, serve, http.MethodDelete, false)

	if aclService.IsRoomAllowed("3002") {
		t.Fatal("repeated remove left the room in the whitelist")
	}
}

func TestRoomChangeRejectsStaleACLMode(t *testing.T) {
	serve, aclService := newAdminTestRouter(t, &recordingIrisRooms{})

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		rec := serve(method, roomsPath, `{"room":"3003","mode":"blacklist"}`)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s stale mode = %d %s, want 409", method, rec.Code, rec.Body.String())
		}

		if code := decodeRoomMutation(t, rec.Body.Bytes()).Code; code != "acl_mode_changed" {
			t.Fatalf("%s stale mode code = %q, want acl_mode_changed", method, code)
		}
	}

	_, mode, rooms := aclService.GetACLStatus()
	if mode != "whitelist" || len(rooms) != 0 {
		t.Fatalf("ACL after stale changes = (%s, %v), want unchanged empty whitelist", mode, rooms)
	}

	for _, body := range []string{`{"room":"3003"}`, `{"room":"3003","mode":"graylist"}`, `{"room":"","mode":"whitelist"}`} {
		if rec := serve(http.MethodPost, roomsPath, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("add %s = %d %s, want 400", body, rec.Code, rec.Body.String())
		}
	}
}
