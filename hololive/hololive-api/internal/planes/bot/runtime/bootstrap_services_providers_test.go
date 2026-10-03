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

package botruntime

import (
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	"github.com/kapu/hololive-api/internal/service/acl"
	dbtest "github.com/kapu/hololive-dbtest"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestProvideACLService_UsesDefaultsWhenDBIsEmpty(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)

	logger := slog.New(slog.DiscardHandler)
	dbClient := &dbmocks.Client{
		GetPoolFunc: func() *pgxpool.Pool { return pool },
	}
	service, err := appbootstrap.ProvideACLService(t.Context(), true, acl.ACLModeWhitelist, []string{"1001", "1002"}, dbClient, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	assert.True(t, service.IsReady())

	enabled, _, rooms := service.GetACLStatus()
	assert.True(t, enabled)
	assert.Len(t, rooms, 2)
}
