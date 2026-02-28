package template

import (
	"context"
	"strings"
	"testing"
	"text/template"
)

func TestTemplateFuncs_Add(t *testing.T) {
	addFn := templateFuncs["add"].(func(int, int) int)
	tests := []struct {
		a, b     int
		expected int
	}{
		{1, 2, 3},
		{0, 0, 0},
		{-1, 1, 0},
		{100, 200, 300},
	}

	for _, tt := range tests {
		result := addFn(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("add(%d, %d) = %d, expected %d", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestTemplateFuncs_Dict(t *testing.T) {
	dictFn := templateFuncs["dict"].(func(...any) (map[string]any, error))

	t.Run("valid dict", func(t *testing.T) {
		result, err := dictFn("key1", "value1", "key2", 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result["key1"] != "value1" {
			t.Errorf("expected key1=value1, got %v", result["key1"])
		}
		if result["key2"] != 42 {
			t.Errorf("expected key2=42, got %v", result["key2"])
		}
	})

	t.Run("odd number of arguments", func(t *testing.T) {
		_, err := dictFn("key1", "value1", "key2")
		if err == nil {
			t.Error("expected error for odd number of arguments")
		}
	})

	t.Run("non-string key", func(t *testing.T) {
		_, err := dictFn(123, "value")
		if err == nil {
			t.Error("expected error for non-string key")
		}
	})
}

func TestTemplateFuncs_Truncate(t *testing.T) {
	truncateFn := templateFuncs["truncate"].(func(int, string) string)
	tests := []struct {
		maxLen   int
		input    string
		expected string
	}{
		{10, "short", "short"},
		{5, "hello world", "he..."},
		{3, "abcd", "abc"},
		{2, "abcde", "ab"},
		{50, "한글 테스트 문자열", "한글 테스트 문자열"},
		{5, "한글테스트입니다", "한글..."},
	}

	for _, tt := range tests {
		result := truncateFn(tt.maxLen, tt.input)
		if result != tt.expected {
			t.Errorf("truncate(%d, %q) = %q, expected %q", tt.maxLen, tt.input, result, tt.expected)
		}
	}
}

func TestTemplateFuncs_FormatNumber(t *testing.T) {
	tests := []struct {
		n        int64
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
	}

	for _, tt := range tests {
		result := formatNumber(tt.n)
		if result != tt.expected {
			t.Errorf("formatNumber(%d) = %q, expected %q", tt.n, result, tt.expected)
		}
	}
}

func TestTemplateFuncs_FormatNumberKR(t *testing.T) {
	tests := []struct {
		n        int64
		expected string
	}{
		{999, "999"},
		{1000, "1.0천"},
		{10000, "1.0만"},
		{100000, "10.0만"},
		{1000000, "100.0만"},
		{100000000, "1.0억"},
	}

	for _, tt := range tests {
		result := formatNumberKR(tt.n)
		if result != tt.expected {
			t.Errorf("formatNumberKR(%d) = %q, expected %q", tt.n, result, tt.expected)
		}
	}
}

func TestTemplateFuncs_FormatNumberFuncs_AcceptInt(t *testing.T) {
	tmpl, err := template.New("test").Funcs(templateFuncs).Parse("{{formatNumber .ViewerCount}} {{formatNumberKR .ViewerCount}}")
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	data := struct {
		ViewerCount int
	}{
		ViewerCount: 15000,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	result := buf.String()
	expected := "15,000 1.5만"
	if result != expected {
		t.Errorf("template output = %q, expected %q", result, expected)
	}
}

func TestRender_WithMockDB(t *testing.T) {
	t.Skip("requires database mock - covered by integration tests")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

var _ = context.Background
