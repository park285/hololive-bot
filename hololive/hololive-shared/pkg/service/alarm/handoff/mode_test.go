package handoff

import "testing"

func TestParseMode(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		raw  string
		want Mode
	}{
		{raw: "", want: ModeOff},
		{raw: " OFF ", want: ModeOff},
		{raw: "shadow", want: ModeShadow},
		{raw: "CUTOVER", want: ModeCutover},
	} {
		mode, err := ParseMode(testCase.raw)
		if err != nil {
			t.Fatalf("ParseMode(%q) error = %v", testCase.raw, err)
		}
		if mode != testCase.want {
			t.Fatalf("ParseMode(%q) = %q, want %q", testCase.raw, mode, testCase.want)
		}
	}

	if _, err := ParseMode("dual-write"); err == nil {
		t.Fatal("ParseMode(dual-write) error = nil")
	}
}
