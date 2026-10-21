package irisrooms

import "testing"

func TestListPathIsInternalRoute(t *testing.T) {
	t.Parallel()

	if ListPath != "/internal/iris/rooms" {
		t.Fatalf("ListPath = %q", ListPath)
	}
}
