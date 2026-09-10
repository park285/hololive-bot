package session

import (
	jsonv2 "encoding/json/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIncompleteStoredSessionCannotAuthenticateOrMutate(t *testing.T) {
	for _, field := range []string{"id", "family_id", "created_at", "expires_at", "absolute_expires_at", "last_rotated_at"} {
		t.Run(field, func(t *testing.T) {
			store, mr := newTestStore(t)

			store.cfg.RotationInterval = 0

			missingIdentity := field == "id" || field == "family_id"
			created, err := store.Create(t.Context())
			require.NoError(t, err)

			raw, err := mr.Get(sessionKey(created.ID))
			require.NoError(t, err)

			var record map[string]any

			require.NoError(t, jsonv2.Unmarshal([]byte(raw), &record))
			delete(record, field)

			invalid, err := jsonv2.Marshal(record)
			require.NoError(t, err)
			require.NoError(t, mr.Set(sessionKey(created.ID), string(invalid)))
			mr.SetTTL(sessionKey(created.ID), time.Hour)

			_, found, err := store.Get(t.Context(), created.ID)
			require.Equal(t, missingIdentity, err == nil)
			require.False(t, found)

			refresh, err := store.Refresh(t.Context(), created.ID, false)
			require.Equal(t, missingIdentity, err == nil)
			require.Nil(t, refresh.Session)
			require.NotEqual(t, RefreshRefreshed, refresh.Kind)

			_, rotated, err := store.Rotate(t.Context(), created.ID)
			require.Equal(t, missingIdentity, err == nil)
			require.False(t, rotated)

			// 불완전한 레코드를 현재 형식으로 수리하거나 다른 family를 삭제하지 않습니다.
			err = store.Delete(t.Context(), created.ID)
			require.Equal(t, missingIdentity, err == nil)

			after, err := mr.Get(sessionKey(created.ID))
			require.NoError(t, err)
			require.Equal(t, string(invalid), after)
			require.Equal(t, created.ID, mr.HGet(familyKey(created.FamilyID), "token"))
		})
	}
}
