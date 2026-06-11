package member

import (
	"errors"
	"testing"
)

func TestCollectJoinedRows_HappyPath(t *testing.T) {
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error { return nil }},
		{scan: func(dest ...any) error { return nil }},
	}}

	got, err := collectJoinedRows(rows, "iter", func(r pgxRows) (int, error) {
		return rows.index, nil
	})
	if err != nil {
		t.Fatalf("collectJoinedRows error = %v, want nil", err)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("collected = %#v, want [1 2]", got)
	}
}

func TestCollectJoinedRows_JoinsRowErrorsAndKeepsPartial(t *testing.T) {
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error { return nil }},
		{scan: func(dest ...any) error { return nil }},
		{scan: func(dest ...any) error { return nil }},
	}}

	got, err := collectJoinedRows(rows, "iter", func(r pgxRows) (int, error) {
		if rows.index == 2 {
			return 0, errors.New("scan boom")
		}
		return rows.index, nil
	})
	if err == nil {
		t.Fatal("collectJoinedRows error = nil, want non-nil")
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("collected = %#v, want partial [1 3]", got)
	}
	if !containsAll(err.Error(), []string{"scan boom"}) {
		t.Fatalf("error = %q, want row error joined", err.Error())
	}
}

func TestCollectJoinedRows_JoinsRowsErrWithLabel(t *testing.T) {
	rows := &fakeMemberRows{
		err: errors.New("connection reset"),
		rows: []fakeMemberRow{
			{scan: func(dest ...any) error { return nil }},
		},
	}

	got, err := collectJoinedRows(rows, "widget rows iteration", func(r pgxRows) (int, error) {
		return rows.index, nil
	})
	if err == nil {
		t.Fatal("collectJoinedRows error = nil, want non-nil")
	}
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("collected = %#v, want [1]", got)
	}
	if !containsAll(err.Error(), []string{"widget rows iteration", "connection reset"}) {
		t.Fatalf("error = %q, want labeled rows.Err joined", err.Error())
	}
}
