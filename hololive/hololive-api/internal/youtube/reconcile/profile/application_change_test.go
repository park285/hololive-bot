package profile

import "testing"

func TestReduceUnchangedProfileRecordsNoApplication(t *testing.T) {
	t.Parallel()

	first := profileAt(1, t1(), Field{Present: true, Value: testDescription}, Field{Present: true, Value: "2019-01-02"})
	second := profileAt(2, t2(), Field{}, Field{})
	got := mustReduceAll(t, []Evidence{first, second}, Policy{})

	if len(got.Applications) != 0 {
		t.Fatalf("applications = %#v", got.Applications)
	}
}
