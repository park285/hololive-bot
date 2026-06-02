package configsub_test

import (
	"testing"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	sharedjson "github.com/park285/shared-go/pkg/json"
)

// ConfigUpdate가 ConfigUpdateV1의 type alias임을 컴파일 타임에 강제한다.
// 동일 필드의 별개 struct로 되돌리면(alias 해제) 아래 두 줄이 컴파일 실패한다.
var (
	_ contractssettings.ConfigUpdateV1 = configsub.ConfigUpdate{}
	_ configsub.ConfigUpdate           = contractssettings.ConfigUpdateV1{}
)

func TestH5_ConfigUpdateWireRoundTrip(t *testing.T) {
	t.Parallel()

	orig := configsub.ConfigUpdate{
		Type:    contractssettings.UpdateTypeAlarmAdvanceMinutes,
		Payload: sharedjson.RawMessage(`{"minutes":15}`),
	}
	b, err := sharedjson.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got configsub.ConfigUpdate
	if err := sharedjson.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != orig.Type {
		t.Fatalf("Type = %q, want %q", got.Type, orig.Type)
	}
	if string(got.Payload) != string(orig.Payload) {
		t.Fatalf("Payload = %q, want %q", string(got.Payload), string(orig.Payload))
	}
}
