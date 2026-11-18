package api

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
	dbtest "github.com/kapu/hololive-dbtest"
)

func TestMemberScheduleCardTemplatePersistsOnlyTheSelectedMember(t *testing.T) {
	pool := dbtest.NewPool(t)
	store, err := scheduleposter.NewStore(pool)
	require.NoError(t, err)

	handler := &SchedulePosterHandler{store: store, logger: newDiscardLogger()}
	member := func(slug string) int {
		var id int

		require.NoError(t, pool.QueryRow(t.Context(), `SELECT id FROM members WHERE slug = $1`, slug).Scan(&id))

		return id
	}
	request := func(method string, id int, body string, status int) string {
		t.Helper()

		raw := strconv.Itoa(id)
		ctx, response := newAPITestContext(method, "/api/holo/members/"+raw+"/schedule-card-template", []byte(body))

		ctx.Params = gin.Params{{Key: "id", Value: raw}}
		handler.MemberScheduleCardTemplate(ctx)
		require.Equal(t, status, response.Code, response.Body.String())

		return response.Body.String()
	}

	sopia, kyoko := member("sorashina-sopia"), member("hyakuto-kyoko")

	const (
		toOrbit   = `{"expectedTemplate":"classic","template":"orbit"}`
		toClassic = `{"expectedTemplate":"orbit","template":"classic"}`
	)

	require.JSONEq(t, `{"template":"orbit"}`, request(http.MethodGet, sopia, "", http.StatusOK))
	require.JSONEq(t, `{"template":"classic"}`, request(http.MethodGet, kyoko, "", http.StatusOK))
	require.JSONEq(t, `{"template":"orbit"}`, request(http.MethodPatch, kyoko, toOrbit, http.StatusOK))
	request(http.MethodPatch, kyoko, toOrbit, http.StatusConflict)
	request(http.MethodPatch, kyoko, `{"template":"classic"}`, http.StatusBadRequest)
	request(http.MethodPatch, kyoko, `{"expectedTemplate":"orbit","template":"auto"}`, http.StatusBadRequest)
	request(http.MethodPatch, 2147483647, toOrbit, http.StatusNotFound)
	request(http.MethodPatch, sopia, toClassic, http.StatusOK)

	reader, err := scheduleposter.NewStore(pool)
	require.NoError(t, err)

	saved, err := reader.MemberCardTemplate(t.Context(), kyoko)
	require.NoError(t, err)
	require.Equal(t, cardtemplate.Orbit, saved)
	require.JSONEq(t, `{"template":"classic"}`, request(http.MethodGet, sopia, "", http.StatusOK))
}
