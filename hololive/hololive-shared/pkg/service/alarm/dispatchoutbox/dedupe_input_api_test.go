package dispatchoutbox

import (
	"reflect"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestDedupeInputPublicShape(t *testing.T) {
	type fieldSpec struct {
		name   string
		typeOf reflect.Type
	}
	want := []fieldSpec{
		{name: "RoomID", typeOf: reflect.TypeFor[string]()},
		{name: "ChannelID", typeOf: reflect.TypeFor[string]()},
		{name: "AlarmType", typeOf: reflect.TypeFor[domain.AlarmType]()},
		{name: "StreamID", typeOf: reflect.TypeFor[string]()},
		{name: "Title", typeOf: reflect.TypeFor[string]()},
		{name: "StartScheduled", typeOf: reflect.TypeFor[time.Time]()},
		{name: "MinutesUntil", typeOf: reflect.TypeFor[int]()},
		{name: "ScheduleChangePreviousStart", typeOf: reflect.TypeFor[string]()},
		{name: "Category", typeOf: reflect.TypeFor[string]()},
		{name: "SourceKind", typeOf: reflect.TypeFor[domain.AlarmDispatchSourceKind]()},
		{name: "SourceIdentity", typeOf: reflect.TypeFor[string]()},
		{name: "SourceOutboxKind", typeOf: reflect.TypeFor[domain.OutboxKind]()},
	}

	typeOf := reflect.TypeFor[DedupeInput]()
	if typeOf.NumField() != len(want) {
		t.Fatalf("DedupeInput fields = %d, want %d", typeOf.NumField(), len(want))
	}
	for i, expected := range want {
		field := typeOf.Field(i)
		if field.Name != expected.name || field.Type != expected.typeOf || !field.IsExported() {
			t.Fatalf("DedupeInput field %d = %s %s exported=%t, want %s %s exported=true",
				i, field.Name, field.Type, field.IsExported(), expected.name, expected.typeOf)
		}
	}
}
