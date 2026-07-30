package render

import (
	"bytes"
	"context"
	"testing"
)

func TestHelpCardProviderReturnsApprovedAssetsInOrder(t *testing.T) {
	images, err := NewHelpCardProvider().HelpImages(t.Context())
	if err != nil {
		t.Fatalf("HelpImages() error = %v", err)
	}
	if len(images) != len(approvedHelpCards) {
		t.Fatalf("HelpImages() count = %d, want %d", len(images), len(approvedHelpCards))
	}
	for index, card := range approvedHelpCards {
		if !bytes.Equal(images[index], card.data) {
			t.Fatalf("HelpImages() card %d does not match %q", index, card.id)
		}
	}

	images[0][0] ^= 0xff
	again, err := NewHelpCardProvider().HelpImages(t.Context())
	if err != nil {
		t.Fatalf("second HelpImages() error = %v", err)
	}
	if !bytes.Equal(again[0], approvedHelpCards[0].data) {
		t.Fatal("HelpImages() returned aliased asset data")
	}
}

func TestHelpCardProviderRejectsInvalidContextAndReceiver(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewHelpCardProvider().HelpImages(ctx); err == nil {
		t.Fatal("HelpImages() accepted canceled context")
	}
	var provider *HelpCardProvider
	if _, err := provider.HelpImages(t.Context()); err == nil {
		t.Fatal("HelpImages() accepted nil provider")
	}
}

func TestApprovedHelpCardsMatchCatalogMetadata(t *testing.T) {
	if len(approvedHelpCards) != 3 {
		t.Fatalf("approved help cards = %d, want 3", len(approvedHelpCards))
	}
	wantIDs := []string{"broadcast", "member-alarm-news", "events-more"}
	for index, wantID := range wantIDs {
		card := approvedHelpCards[index]
		if card.id != wantID || card.asset == "" || card.sha256 == "" {
			t.Fatalf("approved help card %d metadata = %#v", index, card)
		}
	}
}
