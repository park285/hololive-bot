package matcher

import "testing"

func TestNormalizeSuffix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "trim and remove 짱", in: "  후부짱 ", want: "후부"},
		{name: "remove 쨩", in: "미코쨩", want: "미코"},
		{name: "no suffix", in: "스이세이", want: "스이세이"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := NormalizeSuffix(tc.in); got != tc.want {
				t.Fatalf("NormalizeSuffix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
