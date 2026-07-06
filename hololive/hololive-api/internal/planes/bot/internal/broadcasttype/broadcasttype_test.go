package broadcasttype

import "testing"

func TestParseNormalizesToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		want Type
	}{
		{raw: "게임", want: Game},
		{raw: "#게임", want: Game},
		{raw: "ｇａｍｅ", want: Game},
		{raw: "ＡＳＭＲ", want: ASMR},
		{raw: "Watch Party", want: Watchalong},
		{raw: "＃競馬", want: HorseRacing},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()

			got, ok := Parse(tt.raw)
			if !ok || got != tt.want {
				t.Fatalf("Parse(%q) = (%q, %v), want (%q, true)", tt.raw, got, ok, tt.want)
			}
		})
	}
}

func TestParseRejectsUnknownToken(t *testing.T) {
	t.Parallel()

	if typ, ok := Parse("정체불명"); ok {
		t.Fatalf("Parse(정체불명) = (%q, true), want ok=false", typ)
	}
}
