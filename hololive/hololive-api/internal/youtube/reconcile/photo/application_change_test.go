package photo

import "testing"

func TestReduceUnidentifiedPhotoRecordsNoApplication(t *testing.T) {
	t.Parallel()

	got := mustReduceAll(t, []Evidence{photoAt(1, t1(), Variant{Kind: testKindAvatar, URL: testPhotoURL})}, Policy{})

	if len(got.Applications) != 0 {
		t.Fatalf("applications = %#v", got.Applications)
	}
}
