package runtimepolicy

import "testing"

func TestPostgresCollectorRoleUser(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"collector-a": PostgresCollectorARoleUser,
		"collector-b": PostgresCollectorBRoleUser,
		"collector-c": PostgresCollectorCRoleUser,
		"collector-d": PostgresCollectorDRoleUser,
	}

	for instanceID, want := range tests {
		if got, ok := PostgresCollectorRoleUser(instanceID); !ok || got != want {
			t.Fatalf("PostgresCollectorRoleUser(%q) = %q, %v; want %q, true", instanceID, got, ok, want)
		}
	}

	if got, ok := PostgresCollectorRoleUser("collector-e"); ok || got != "" {
		t.Fatalf("unsupported collector role = %q, %v; want empty, false", got, ok)
	}
}
