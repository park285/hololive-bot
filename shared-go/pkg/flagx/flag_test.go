package flagx

import (
	"errors"
	"testing"
)

func TestFlag_Validate(t *testing.T) {
	tests := []struct {
		name    string
		flag    Flag
		wantErr error
	}{
		{
			name:    "valid flag",
			flag:    Flag("active"),
			wantErr: nil,
		},
		{
			name:    "empty flag",
			flag:    Flag(""),
			wantErr: ErrEmptyFlag,
		},
		{
			name:    "whitespace only is valid",
			flag:    Flag(" "),
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.flag.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFlag_String(t *testing.T) {
	flag := Flag("test_flag")
	if got := flag.String(); got != "test_flag" {
		t.Errorf("String() = %q, want %q", got, "test_flag")
	}
}

func TestNewFlagSet(t *testing.T) {
	tests := []struct {
		name  string
		flags []Flag
		want  int
	}{
		{
			name:  "empty set",
			flags: nil,
			want:  0,
		},
		{
			name:  "single flag",
			flags: []Flag{"active"},
			want:  1,
		},
		{
			name:  "multiple flags",
			flags: []Flag{"active", "verified", "premium"},
			want:  3,
		},
		{
			name:  "duplicate flags",
			flags: []Flag{"active", "active", "active"},
			want:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewFlagSet(tt.flags...)
			if got := fs.Len(); got != tt.want {
				t.Errorf("NewFlagSet() Len() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFlagSet_Add(t *testing.T) {
	t.Run("add new flag", func(t *testing.T) {
		fs := NewFlagSet()
		fs.Add("active")
		if !fs.Has("active") {
			t.Error("Add() flag not found after adding")
		}
		if fs.Len() != 1 {
			t.Errorf("Len() = %d, want 1", fs.Len())
		}
	})

	t.Run("add duplicate flag (idempotent)", func(t *testing.T) {
		fs := NewFlagSet("active")
		fs.Add("active")
		fs.Add("active")
		if fs.Len() != 1 {
			t.Errorf("Len() = %d, want 1 after duplicate adds", fs.Len())
		}
	})

	t.Run("add multiple flags", func(t *testing.T) {
		fs := NewFlagSet()
		fs.Add("a")
		fs.Add("b")
		fs.Add("c")
		if fs.Len() != 3 {
			t.Errorf("Len() = %d, want 3", fs.Len())
		}
	})
}

func TestFlagSet_Remove(t *testing.T) {
	t.Run("remove existing flag", func(t *testing.T) {
		fs := NewFlagSet("active", "verified")
		fs.Remove("active")
		if fs.Has("active") {
			t.Error("Remove() flag still exists after removing")
		}
		if fs.Len() != 1 {
			t.Errorf("Len() = %d, want 1", fs.Len())
		}
	})

	t.Run("remove non-existing flag (idempotent)", func(t *testing.T) {
		fs := NewFlagSet("active")
		fs.Remove("nonexistent")
		if fs.Len() != 1 {
			t.Errorf("Len() = %d, want 1 after removing non-existent", fs.Len())
		}
	})

	t.Run("remove from empty set", func(t *testing.T) {
		fs := NewFlagSet()
		fs.Remove("any")
		if fs.Len() != 0 {
			t.Errorf("Len() = %d, want 0", fs.Len())
		}
	})
}

func TestFlagSet_Has(t *testing.T) {
	fs := NewFlagSet("active", "verified")

	tests := []struct {
		name string
		flag Flag
		want bool
	}{
		{"existing flag", "active", true},
		{"another existing flag", "verified", true},
		{"non-existing flag", "premium", false},
		{"empty flag", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fs.Has(tt.flag); got != tt.want {
				t.Errorf("Has(%q) = %v, want %v", tt.flag, got, tt.want)
			}
		})
	}
}

func TestFlagSet_List(t *testing.T) {
	t.Run("empty set", func(t *testing.T) {
		fs := NewFlagSet()
		list := fs.List()
		if list != nil {
			t.Errorf("List() = %v, want nil for empty set", list)
		}
	})

	t.Run("single flag", func(t *testing.T) {
		fs := NewFlagSet("active")
		list := fs.List()
		if len(list) != 1 || list[0] != "active" {
			t.Errorf("List() = %v, want [active]", list)
		}
	})

	t.Run("multiple flags sorted", func(t *testing.T) {
		fs := NewFlagSet("z", "a", "m")
		list := fs.List()
		if len(list) != 3 {
			t.Errorf("List() len = %d, want 3", len(list))
		}
		if list[0] != "a" || list[1] != "m" || list[2] != "z" {
			t.Errorf("List() = %v, want [a m z]", list)
		}
	})
}

func TestFlagSet_Len(t *testing.T) {
	tests := []struct {
		name  string
		flags []Flag
		want  int
	}{
		{"empty", nil, 0},
		{"one", []Flag{"a"}, 1},
		{"three", []Flag{"a", "b", "c"}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := NewFlagSet(tt.flags...)
			if got := fs.Len(); got != tt.want {
				t.Errorf("Len() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestErrEmptyFlag(t *testing.T) {
	if ErrEmptyFlag.Error() != "flagx: flag cannot be empty" {
		t.Errorf("ErrEmptyFlag = %q, want %q", ErrEmptyFlag.Error(), "flagx: flag cannot be empty")
	}
}
