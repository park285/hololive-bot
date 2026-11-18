package cache

import (
	"context"
	"testing"
)

func TestCompareAndDelete(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*Service, context.Context)
		key        string
		expected   string
		wantResult bool
	}{
		{
			name: "value matches - should delete and return true",
			setup: func(service *Service, ctx context.Context) {
				cmd := service.client.B().Set().Key("cas:match").Value("secret-token").Build()
				service.client.Do(ctx, cmd)
			},
			key:        "cas:match",
			expected:   "secret-token",
			wantResult: true,
		},
		{
			name: "value does not match - should not delete and return false",
			setup: func(service *Service, ctx context.Context) {
				cmd := service.client.B().Set().Key("cas:mismatch").Value("actual-value").Build()
				service.client.Do(ctx, cmd)
			},
			key:        "cas:mismatch",
			expected:   "wrong-value",
			wantResult: false,
		},
		{
			name:       "key does not exist - should return false",
			setup:      nil,
			key:        "cas:nonexistent",
			expected:   "any-value",
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, _ := newTestCacheService(t)
			ctx := t.Context()

			if tt.setup != nil {
				tt.setup(service, ctx)
			}

			got, err := service.CompareAndDelete(ctx, tt.key, tt.expected)
			if err != nil {
				t.Fatalf("CompareAndDelete() error = %v", err)
			}

			if got != tt.wantResult {
				t.Errorf("CompareAndDelete() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}
