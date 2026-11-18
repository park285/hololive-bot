package store

import (
	"reflect"
	"testing"
)

func TestUniqueStrings(t *testing.T) {
	t.Parallel()

	in := []string{"room1", "room2", "room1", "room3", "room2"}
	got := UniqueStrings(in)
	want := []string{"room1", "room2", "room3"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UniqueStrings() = %#v, want %#v", got, want)
	}

	if out := UniqueStrings(nil); out != nil {
		t.Fatalf("UniqueStrings(nil) = %#v, want nil", out)
	}
}
