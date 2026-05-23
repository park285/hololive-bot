package settings

import (
	"testing"
)

func TestParseCORSAllowedOrigins_NonProduction_EmptyReturnsDefault(t *testing.T) {
	t.Parallel()

	origins, blocked := parseCORSAllowedOrigins("", false)
	if blocked {
		t.Fatal("non-production should not block")
	}
	if len(origins) != 1 || origins[0] != "http://localhost:5173" {
		t.Fatalf("origins = %v, want [http://localhost:5173]", origins)
	}
}

func TestParseCORSAllowedOrigins_NonProduction_CustomOrigins(t *testing.T) {
	t.Parallel()

	origins, blocked := parseCORSAllowedOrigins("http://dev.local,http://test.local", false)
	if blocked {
		t.Fatal("non-production should not block")
	}
	if len(origins) != 2 {
		t.Fatalf("origins count = %d, want 2", len(origins))
	}
}

func TestParseCORSAllowedOrigins_Production_FiltersDangerous(t *testing.T) {
	t.Parallel()

	origins, blocked := parseCORSAllowedOrigins("*,http://localhost:3000,https://safe.example.com", true)
	if blocked {
		t.Fatal("should not be blocked when safe origin remains")
	}
	if len(origins) != 1 || origins[0] != "https://safe.example.com" {
		t.Fatalf("origins = %v, want [https://safe.example.com]", origins)
	}
}

func TestParseCORSAllowedOrigins_Production_AllFiltered(t *testing.T) {
	t.Parallel()

	_, blocked := parseCORSAllowedOrigins("*,http://localhost:3000", true)
	if !blocked {
		t.Fatal("should be blocked when all origins are filtered in production")
	}
}
