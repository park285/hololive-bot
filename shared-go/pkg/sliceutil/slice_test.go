package sliceutil

import (
	"testing"
)

func TestUnique(t *testing.T) {
	tests := []struct {
		name     string
		input    []int
		expected []int
	}{
		{
			name:     "empty slice",
			input:    []int{},
			expected: []int{},
		},
		{
			name:     "single element",
			input:    []int{1},
			expected: []int{1},
		},
		{
			name:     "no duplicates",
			input:    []int{1, 2, 3, 4},
			expected: []int{1, 2, 3, 4},
		},
		{
			name:     "all duplicates",
			input:    []int{5, 5, 5, 5},
			expected: []int{5},
		},
		{
			name:     "mixed duplicates",
			input:    []int{1, 2, 2, 3, 1, 4, 3},
			expected: []int{1, 2, 3, 4},
		},
		{
			name:     "preserves order",
			input:    []int{3, 1, 2, 1, 3},
			expected: []int{3, 1, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Unique(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("Unique(%v) length = %d, want %d", tt.input, len(result), len(tt.expected))
				return
			}

			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("Unique(%v) = %v, want %v", tt.input, result, tt.expected)
					break
				}
			}
		})
	}
}

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty string slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "string duplicates",
			input:    []string{"apple", "banana", "apple", "cherry", "banana"},
			expected: []string{"apple", "banana", "cherry"},
		},
		{
			name:     "case sensitive",
			input:    []string{"Go", "go", "GO"},
			expected: []string{"Go", "go", "GO"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Unique(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("Unique(%v) length = %d, want %d", tt.input, len(result), len(tt.expected))
				return
			}

			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("Unique(%v) = %v, want %v", tt.input, result, tt.expected)
					break
				}
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		slice    []int
		item     int
		expected bool
	}{
		{
			name:     "empty slice",
			slice:    []int{},
			item:     1,
			expected: false,
		},
		{
			name:     "single element found",
			slice:    []int{5},
			item:     5,
			expected: true,
		},
		{
			name:     "single element not found",
			slice:    []int{5},
			item:     3,
			expected: false,
		},
		{
			name:     "item at beginning",
			slice:    []int{1, 2, 3, 4},
			item:     1,
			expected: true,
		},
		{
			name:     "item in middle",
			slice:    []int{1, 2, 3, 4},
			item:     3,
			expected: true,
		},
		{
			name:     "item at end",
			slice:    []int{1, 2, 3, 4},
			item:     4,
			expected: true,
		},
		{
			name:     "item not found",
			slice:    []int{1, 2, 3, 4},
			item:     99,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Contains(tt.slice, tt.item)
			if result != tt.expected {
				t.Errorf("Contains(%v, %d) = %v, want %v", tt.slice, tt.item, result, tt.expected)
			}
		})
	}
}

func TestContainsStrings(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		item     string
		expected bool
	}{
		{
			name:     "empty string slice",
			slice:    []string{},
			item:     "test",
			expected: false,
		},
		{
			name:     "string found",
			slice:    []string{"alice", "bob", "charlie"},
			item:     "bob",
			expected: true,
		},
		{
			name:     "string not found",
			slice:    []string{"alice", "bob", "charlie"},
			item:     "dave",
			expected: false,
		},
		{
			name:     "case sensitive string",
			slice:    []string{"Go", "Python", "Rust"},
			item:     "go",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Contains(tt.slice, tt.item)
			if result != tt.expected {
				t.Errorf("Contains(%v, %q) = %v, want %v", tt.slice, tt.item, result, tt.expected)
			}
		})
	}
}

func BenchmarkUnique(b *testing.B) {
	input := make([]int, 1000)
	for i := range 1000 {
		input[i] = i % 100
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Unique(input)
	}
}

func BenchmarkContains(b *testing.B) {
	slice := make([]int, 1000)
	for i := range 1000 {
		slice[i] = i
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Contains(slice, 999)
	}
}
