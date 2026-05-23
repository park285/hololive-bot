// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package membernews

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
)

func (r *Repository) WarmupCacheFromDB(ctx context.Context) error {
	rooms, err := r.ListSubscribedRooms(ctx)
	if err != nil {
		return fmt.Errorf("list subscribed rooms for warmup: %w", err)
	}

	if r.cache == nil {
		return nil
	}

	r.clearMemberNewsRoomCache(ctx)

	if len(rooms) == 0 {
		return nil
	}

	r.loadMemberNewsRoomsCache(ctx, rooms)
	return nil
}

func (r *Repository) clearMemberNewsRoomCache(ctx context.Context) {
	if err := r.cache.Del(ctx, memberNewsRoomsKey); err != nil {
		r.log.Warn("MemberNews warmup: failed to clear rooms set",
			slog.String("key", memberNewsRoomsKey),
			slog.String("error", err.Error()),
		)
	}
	if err := r.cache.Del(ctx, memberNewsRoomNamesKey); err != nil {
		r.log.Warn("MemberNews warmup: failed to clear room names hash",
			slog.String("key", memberNewsRoomNamesKey),
			slog.String("error", err.Error()),
		)
	}
}

func (r *Repository) loadMemberNewsRoomsCache(ctx context.Context, rooms []model.SubscribedRoom) {
	roomIDs := make([]string, 0, len(rooms))
	nameFields := make(map[string]any, len(rooms))
	for _, room := range rooms {
		roomIDs = append(roomIDs, room.RoomID)
		nameFields[room.RoomID] = room.RoomName
	}

	if _, err := r.cache.SAdd(ctx, memberNewsRoomsKey, roomIDs); err != nil {
		r.log.Warn("MemberNews warmup: failed to load rooms set",
			slog.Int("count", len(roomIDs)),
			slog.String("error", err.Error()),
		)
	}

	if err := r.cache.HMSet(ctx, memberNewsRoomNamesKey, nameFields); err != nil {
		r.log.Warn("MemberNews warmup: failed to load room names hash",
			slog.Int("count", len(nameFields)),
			slog.String("error", err.Error()),
		)
	}
}

func (r *Repository) writeThroughSubscribe(ctx context.Context, roomID, roomName string) {
	if r.cache == nil {
		return
	}

	if _, err := r.cache.SAdd(ctx, memberNewsRoomsKey, []string{roomID}); err != nil {
		r.log.Warn("MemberNews subscribe write-through SADD failed",
			slog.String("room_id", roomID),
			slog.String("error", err.Error()),
		)
	}

	if err := r.cache.HSet(ctx, memberNewsRoomNamesKey, roomID, roomName); err != nil {
		r.log.Warn("MemberNews subscribe write-through HSET failed",
			slog.String("room_id", roomID),
			slog.String("error", err.Error()),
		)
	}
}

func (r *Repository) writeThroughUnsubscribe(ctx context.Context, roomID string) {
	if r.cache == nil {
		return
	}

	if _, err := r.cache.SRem(ctx, memberNewsRoomsKey, []string{roomID}); err != nil {
		r.log.Warn("MemberNews unsubscribe write-through SREM failed",
			slog.String("room_id", roomID),
			slog.String("error", err.Error()),
		)
	}

	if err := r.cache.HDel(ctx, memberNewsRoomNamesKey, roomID); err != nil {
		r.log.Warn("MemberNews unsubscribe write-through HDEL failed",
			slog.String("room_id", roomID),
			slog.String("error", err.Error()),
		)
	}
}
