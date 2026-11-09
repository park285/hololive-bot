package dbtest

import "testing"

func TestHoloANBootstrapKeepsGroupAsChannelRepresentative(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)

	var slug string

	err := pool.QueryRow(t.Context(), `SELECT slug FROM members WHERE channel_id='UCozx5csNhCx1wsVq3SZVkBQ' ORDER BY id LIMIT 1`).Scan(&slug)
	if err != nil {
		t.Fatal(err)
	}

	if slug != "holoan-room" {
		t.Fatalf("fresh bootstrap representative=%q", slug)
	}
}
