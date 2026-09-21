package xspaces

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectionOutputDiagnosticBoundary(t *testing.T) {
	_, err := decodeCollectionOutput([]byte(`{"error":"authentication","http_status":403,"api_codes":[32]}`), errors.New("process failed"))
	failure, ok := errors.AsType[*CollectionError](err)

	if !ok || failure == nil {
		t.Fatalf("expected CollectionError, got %T", err)
	}

	require.Equal(t, "authentication", failure.Code)
	require.Equal(t, 403, failure.HTTPStatus)
	require.Equal(t, []int{32}, failure.APICodes)
	require.Contains(t, failure.Error(), "http_status=403 api_codes=[32]")

	for _, raw := range []string{
		`{"error":"authentication_pending"}`,
		`{"error":"authentication","http_status":99}`,
		`{"error":"authentication","http_status":600}`,
		`{"error":"authentication","api_codes":[-1]}`,
		`{"error":"authentication","api_codes":[65536]}`,
		`{"error":"authentication","api_codes":[1,2,3,4,5,6,7,8,9]}`,
		`{"error":"authentication","api_codes":["secret"]}`,
		`{"error":"authentication","message":"secret"}`,
		`{"error":"secret"}`,
	} {
		_, err := decodeCollectionOutput([]byte(raw), nil)
		failure, ok := errors.AsType[*CollectionError](err)

		if !ok || failure == nil {
			t.Fatalf("expected CollectionError, got %T", err)
		}

		require.Contains(t, []string{"invalid_response", "collector_failed"}, failure.Code)
		require.NotContains(t, failure.Error(), "secret")
	}
}

func TestCollectionOutputPreservesFailureAndEmptySuccess(t *testing.T) {
	spaces, err := decodeCollectionOutput([]byte(`{"spaces":[]}`), nil)
	require.NoError(t, err)
	require.Empty(t, spaces)

	_, err = decodeCollectionOutput([]byte(`{"spaces":[]}`), errors.New("process failed"))
	require.Error(t, err)

	_, err = decodeCollectionOutput([]byte(`{}`), nil)
	require.Error(t, err)
}
