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

package api

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

// DEC-20260926-stack-hololive-room-acl-and-console-contract: 멤버 목록의 aliases는 별명이 없어도 항상
// 빈 ko·ja 배열을 가진 객체다. 소비자인 iris-console은 부재·null 보정을 지우고 null을 거절하므로 소유 API가 이 계약을 지킨다.
func TestGetMembersAlwaysReturnsAliasesObject(t *testing.T) {
	gin.SetMode(gin.TestMode)

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	repo := member.NewMemberRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)

	cache, err := member.NewMemberCache(t.Context(), repo, nil, logger, member.CacheConfig{})
	if err != nil {
		t.Fatal(err)
	}

	handler := &MemberHandler{Handler: &Handler{repository: repo, memberCache: cache, logger: logger}}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	ctx.Request = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/holo/members",
		bytes.NewBufferString(`{"name":"Alias Contract Member","channelId":"UC_alias_contract","isGraduated":false}`))
	handler.AddMember(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/members", nil)
	handler.GetMembers(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("read status=%d", recorder.Code)
	}

	var response struct {
		Members []map[string]any `json:"members"`
	}

	if err := jsonv2.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}

	for _, actual := range response.Members {
		if actual["name"] != "Alias Contract Member" {
			continue
		}

		want := map[string]any{"ko": []any{}, "ja": []any{}}
		if !reflect.DeepEqual(actual["aliases"], want) {
			t.Fatalf("aliases = %#v, want empty ko/ja arrays", actual["aliases"])
		}

		return
	}

	t.Fatal("created member absent")
}
