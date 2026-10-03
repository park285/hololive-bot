package content

import "testing"

func TestMarshalCoverageRejectsEmpty(t *testing.T) {
	t.Parallel()

	if _, err := MarshalCoverage(CoverageValue{}); err == nil {
		t.Fatal("empty coverage must fail closed")
	}
}
