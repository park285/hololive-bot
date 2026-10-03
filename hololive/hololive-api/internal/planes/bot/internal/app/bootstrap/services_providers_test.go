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

package bootstrap

import (
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/service/acl"
	dbtest "github.com/kapu/hololive-dbtest"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestProvideACLServiceWrapsInitializationErrorForNilPostgres(t *testing.T) {
	t.Parallel()

	service, err := ProvideACLService(
		t.Context(),
		true,
		acl.ACLModeWhitelist,
		[]string{testRoomA},
		nil,
		slog.New(slog.DiscardHandler),
	)

	require.Nil(t, service)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to create ACL service")
	assert.ErrorContains(t, err, "postgres service is nil")
}

func newACLPostgresMock(t *testing.T) *databasemocks.Client {
	t.Helper()

	pool := dbtest.NewPool(t)

	return &databasemocks.Client{
		GetPoolFunc: func() *pgxpool.Pool { return pool },
	}
}
