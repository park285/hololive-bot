package matcher

import (
	"testing"
)

func TestParseNameWithOrg(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedName string
		expectedOrg  string
	}{
		{
			name:         "without org",
			input:        "미코",
			expectedName: "미코",
			expectedOrg:  "",
		},
		{
			name:         "with org Hololive",
			input:        "미코 (Hololive)",
			expectedName: "미코",
			expectedOrg:  "Hololive",
		},
		{
			name:         "with org Nijisanji",
			input:        "미코 (Nijisanji)",
			expectedName: "미코",
			expectedOrg:  "Nijisanji",
		},
		{
			name:         "with org VSPO",
			input:        "치호 (VSPO)",
			expectedName: "치호",
			expectedOrg:  "VSPO",
		},
		{
			name:         "with org Indie",
			input:        "사쿠나 (Indie)",
			expectedName: "사쿠나",
			expectedOrg:  "Indie",
		},
		{
			name:         "with extra spaces",
			input:        "미코  (Hololive) ",
			expectedName: "미코",
			expectedOrg:  "Hololive",
		},
		{
			name:         "name with spaces",
			input:        "사쿠라 미코 (Hololive)",
			expectedName: "사쿠라 미코",
			expectedOrg:  "Hololive",
		},
		{
			name:         "empty input",
			input:        "",
			expectedName: "",
			expectedOrg:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotOrg := ParseNameWithOrg(tt.input)
			if gotName != tt.expectedName {
				t.Errorf("ParseNameWithOrg(%q) name = %q, want %q", tt.input, gotName, tt.expectedName)
			}
			if gotOrg != tt.expectedOrg {
				t.Errorf("ParseNameWithOrg(%q) org = %q, want %q", tt.input, gotOrg, tt.expectedOrg)
			}
		})
	}
}
