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

package app

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
)

func TestProvideACLService_UsesDefaultsWhenDBIsEmpty(t *testing.T) {
	t.Parallel()

	dsn := fmt.Sprintf("file:app_acl_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&acl.Settings{}, &acl.Room{}))

	logger := slog.New(slog.DiscardHandler)
	dbClient := &dbmocks.Client{
		GetGormDBFunc: func() *gorm.DB { return db },
	}
	cacheSvc := &cachemocks.Client{
		SetFunc: func(context.Context, string, any, time.Duration) error { return nil },
		DelFunc: func(context.Context, string) error { return nil },
		SAddFunc: func(context.Context, string, []string) (int64, error) {
			return 1, nil
		},
	}

	svc, err := ProvideACLService(t.Context(), true, acl.ACLModeWhitelist, []string{"room-a", "room-b"}, dbClient, cacheSvc, logger)
	require.NoError(t, err)
	require.NotNil(t, svc)
	assert.True(t, svc.IsReady())

	enabled, _, rooms := svc.GetACLStatus()
	assert.True(t, enabled)
	assert.Len(t, rooms, 2)
}

func TestProvideActivityLogger_StdoutOnlyMode(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	activityLogger := ProvideActivityLogger(logger)
	require.NotNil(t, activityLogger)

	activityLogger.Log("test", "summary", map[string]any{"k": "v"})

	logs, err := activityLogger.GetRecentLogs(10)
	require.NoError(t, err)
	assert.Empty(t, logs)
}
