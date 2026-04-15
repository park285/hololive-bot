package providers

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"unsafe"

	"github.com/kapu/hololive-shared/pkg/service/member"
)

func TestProvideMemberServiceAdapter_DetachesCanceledBuildContext(t *testing.T) {
	t.Parallel()

	buildCtx, cancel := context.WithCancel(context.Background())
	cancel()

	provider := ProvideMemberServiceAdapter(
		buildCtx,
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	adapterCtx := providerAdapterContext(t, provider)
	if err := adapterCtx.Err(); err != nil {
		t.Fatalf("adapter ctx err = %v, want nil", err)
	}
}

func providerAdapterContext(t *testing.T, provider member.DataProvider) context.Context {
	t.Helper()

	adapter, ok := provider.(*member.ServiceAdapter)
	if !ok {
		t.Fatalf("provider type = %T, want *member.ServiceAdapter", provider)
	}

	field := reflect.ValueOf(adapter).Elem().FieldByName("ctx")
	if !field.IsValid() {
		t.Fatal("ctx field must exist")
	}

	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	ctx, ok := field.Interface().(context.Context)
	if !ok {
		t.Fatalf("ctx field type = %T, want context.Context", field.Interface())
	}

	return ctx
}
