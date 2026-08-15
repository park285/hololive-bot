package kakaoroom

import (
	"context"
	"errors"
	"testing"

	"github.com/park285/iris-client-go/iris"
)

type stubLister struct {
	rooms []Facts
	err   error
}

func (s stubLister) GetRooms(context.Context) ([]Facts, error) {
	return s.rooms, s.err
}

func TestCatalogObserveOpenAndRegular(t *testing.T) {
	t.Parallel()

	c := New(nil, nil, nil)
	ctx := t.Context()
	c.Observe(ctx, "1", "OM", "")
	c.Observe(ctx, "2", "MultiChat", "")

	if !c.OpenChat(ctx, "1") {
		t.Fatal("open room should use markdown")
	}
	if c.OpenChat(ctx, "2") {
		t.Fatal("regular room should not use markdown")
	}
}

func TestCatalogSkipsBlankObservation(t *testing.T) {
	t.Parallel()

	c := New(nil, stubLister{rooms: []Facts{{RoomID: "3", RoomType: "OM"}}}, nil)
	ctx := t.Context()
	c.Observe(ctx, "3", "", "")

	if !c.OpenChat(ctx, "3") {
		t.Fatal("blank observe must not hide iris open room")
	}
}

func TestCatalogFallsBackToIrisList(t *testing.T) {
	t.Parallel()

	c := New(nil, stubLister{rooms: []Facts{{RoomID: "9", RoomType: "DirectChat"}}}, nil)
	if c.OpenChat(t.Context(), "9") {
		t.Fatal("direct chat from iris must be plain")
	}
}

func TestCatalogUnknownRoomIsPlain(t *testing.T) {
	t.Parallel()

	c := New(nil, stubLister{}, nil)
	if c.OpenChat(t.Context(), "missing") {
		t.Fatal("unknown room must be plain")
	}
}

func TestCatalogIrisErrorIsPlain(t *testing.T) {
	t.Parallel()

	c := New(nil, stubLister{err: errors.New("iris down")}, nil)
	if c.OpenChat(t.Context(), "1") {
		t.Fatal("iris failure must be plain")
	}
}

type errStore struct {
	err error
}

func (s errStore) upsert(context.Context, Facts) error { return s.err }

func (s errStore) get(context.Context, string) (Facts, bool, error) {
	return Facts{}, false, s.err
}

func TestCatalogRefreshSkipsBlankTypeAndLink(t *testing.T) {
	t.Parallel()

	c := New(nil, stubLister{rooms: []Facts{
		{RoomID: "2"},
		{RoomID: "3", RoomType: "OM", RoomLinkID: "link"},
	}}, nil)
	ctx := t.Context()
	c.Observe(ctx, "2", "OM", "keep")
	if !c.OpenChat(ctx, "3") {
		t.Fatal("iris open room should still resolve")
	}
	if !c.OpenChat(ctx, "2") {
		t.Fatal("refresh must not clobber an observed open room with a blank iris row")
	}
}

func TestCatalogDBErrorDoesNotUseIrisFallback(t *testing.T) {
	t.Parallel()

	c := New(nil, stubLister{rooms: []Facts{{RoomID: "1", RoomType: "OM"}}}, nil)
	c.store = errStore{err: errors.New("db down")}
	if c.OpenChat(t.Context(), "1") {
		t.Fatal("database error must not treat iris rooms as found")
	}
}

func TestFactsFromSummary(t *testing.T) {
	t.Parallel()

	roomType := "OM"
	linkID := int64(77)
	got := factsFromSummary(iris.RoomSummary{ChatID: 12, Type: &roomType, LinkID: &linkID})
	if got.RoomID != "12" || got.RoomType != "OM" || got.RoomLinkID != "77" || !got.OpenChat() {
		t.Fatalf("factsFromSummary() = %+v", got)
	}

	openType := "MultiChat"
	linkURL := "https://open.kakao.com/o/test"
	got = factsFromSummary(iris.RoomSummary{ChatID: 13, Type: &openType, LinkURL: &linkURL})
	if !got.OpenChat() || got.RoomLinkID != linkURL {
		t.Fatalf("open link summary = %+v", got)
	}
}
