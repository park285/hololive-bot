package ptrutil

import "testing"

func TestPtr(t *testing.T) {
	s := "test"
	ptr := new(s)
	if *ptr != s {
		t.Errorf("Ptr() = %v, want %v", *ptr, s)
	}
}

func TestString(t *testing.T) {
	s := "hello"
	ptr := new(s)
	if *ptr != s {
		t.Errorf("String() = %v, want %v", *ptr, s)
	}
}

func TestDeref(t *testing.T) {
	s := "test"
	ptr := &s
	if got := Deref(ptr); got != s {
		t.Errorf("Deref() = %v, want %v", got, s)
	}

	var nilPtr *string
	if got := Deref(nilPtr); got != "" {
		t.Errorf("Deref(nil) = %v, want empty string", got)
	}
}

func TestDerefOr(t *testing.T) {
	var nilPtr *string
	if got := DerefOr(nilPtr, "default"); got != "default" {
		t.Errorf("DerefOr(nil) = %v, want default", got)
	}

	s := "actual"
	if got := DerefOr(&s, "default"); got != "actual" {
		t.Errorf("DerefOr(&s) = %v, want actual", got)
	}
}
