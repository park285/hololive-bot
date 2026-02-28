package server

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestSplitChannelIDs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single channel",
			input:    "UC123",
			expected: []string{"UC123"},
		},
		{
			name:     "multiple channels",
			input:    "UC123,UC456,UC789",
			expected: []string{"UC123", "UC456", "UC789"},
		},
		{
			name:     "with spaces",
			input:    "UC123, UC456 , UC789",
			expected: []string{"UC123", "UC456", "UC789"},
		},
		{
			name:     "with tabs",
			input:    "UC123,\tUC456",
			expected: []string{"UC123", "UC456"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "only commas",
			input:    ",,,",
			expected: []string{},
		},
		{
			name:     "trailing comma",
			input:    "UC123,UC456,",
			expected: []string{"UC123", "UC456"},
		},
		{
			name:     "leading comma",
			input:    ",UC123,UC456",
			expected: []string{"UC123", "UC456"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitChannelIDs(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("splitChannelIDs(%q) = %v, want %v", tt.input, got, tt.expected)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("splitChannelIDs(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestSplitByComma(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple",
			input:    "a,b,c",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "no comma",
			input:    "abc",
			expected: []string{"abc"},
		},
		{
			name:     "empty",
			input:    "",
			expected: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitByComma(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("splitByComma(%q) = %v, want %v", tt.input, got, tt.expected)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("splitByComma(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestTrimSpace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no whitespace",
			input:    "test",
			expected: "test",
		},
		{
			name:     "leading spaces",
			input:    "   test",
			expected: "test",
		},
		{
			name:     "trailing spaces",
			input:    "test   ",
			expected: "test",
		},
		{
			name:     "both sides",
			input:    "  test  ",
			expected: "test",
		},
		{
			name:     "tabs",
			input:    "\ttest\t",
			expected: "test",
		},
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimSpace(tt.input)
			if got != tt.expected {
				t.Errorf("trimSpace(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestMemberToChannelResponse(t *testing.T) {
	tests := []struct {
		name     string
		member   *domain.Member
		expected *ChannelResponse
	}{
		{
			name:     "nil member",
			member:   nil,
			expected: nil,
		},
		{
			name: "member with photo",
			member: &domain.Member{
				ChannelID: "UC123",
				Name:      "Test Member",
				Photo:     "https://example.com/photo.jpg",
			},
			expected: &ChannelResponse{
				ID:    "UC123",
				Name:  "Test Member",
				Photo: new("https://example.com/photo.jpg"),
			},
		},
		{
			name: "member without photo",
			member: &domain.Member{
				ChannelID: "UC456",
				Name:      "No Photo Member",
				Photo:     "",
			},
			expected: &ChannelResponse{
				ID:    "UC456",
				Name:  "No Photo Member",
				Photo: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := memberToChannelResponse(tt.member)
			if tt.expected == nil {
				if got != nil {
					t.Errorf("memberToChannelResponse() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Errorf("memberToChannelResponse() = nil, want %+v", tt.expected)
				return
			}
			if got.ID != tt.expected.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.expected.ID)
			}
			if got.Name != tt.expected.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.expected.Name)
			}
			if tt.expected.Photo == nil {
				if got.Photo != nil {
					t.Errorf("Photo = %v, want nil", *got.Photo)
				}
			} else {
				if got.Photo == nil {
					t.Errorf("Photo = nil, want %q", *tt.expected.Photo)
				} else if *got.Photo != *tt.expected.Photo {
					t.Errorf("Photo = %q, want %q", *got.Photo, *tt.expected.Photo)
				}
			}
		})
	}
}
