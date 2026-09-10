package holo

import (
	jsonv2 "encoding/json/v2"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/contract"
)

const memberFixture = `{"id":9007199254740993,"channelId":"UC-fixture","name":"한글😀","isGraduated":false,"sync_source":"synthetic-private"}`

func memberFixtureClient(t *testing.T, body string) *Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_, err := w.Write([]byte(body))
		if err != nil {
			t.Errorf("write fixture: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL, "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	return client
}

func TestMembersProjectionDropsUnownedFieldsAndPreservesID(t *testing.T) {
	client := memberFixtureClient(t, `{"status":"ok","members":[`+memberFixture+`],"internal":"synthetic-private"}`)
	response, err := client.GetMembers(t.Context())
	require.NoError(t, err)

	members := decodedMemberList(t, response)
	require.Len(t, members, 1)
	require.Equal(t, "9007199254740993", members[0].ID)
	require.Equal(t, "한글😀", members[0].Name)
	require.NotNil(t, members[0].Aliases.KO)
	require.Empty(t, members[0].Aliases.KO)

	data, err := jsonv2.Marshal(response)
	require.NoError(t, err)
	require.NotContains(t, string(data), "synthetic-private")
}

func TestMembersProjectionRejectsMissingDataWithoutDefaulting(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"status":"ok","members":null}`, `{"status":"ok","members":[null]}`, `{"status":"ok","members":[{"id":1}]}`, `{"status":"ok","members":[` + strings.Replace(memberFixture, "9007199254740993", "9223372036854775808", 1) + `]}`} {
		_, err := memberFixtureClient(t, body).GetMembers(t.Context())
		require.Error(t, err)
	}

	response, err := memberFixtureClient(t, `{"status":"ok","members":[]}`).GetMembers(t.Context())
	require.NoError(t, err)

	members := decodedMemberList(t, response)
	require.NotNil(t, members)
	require.Empty(t, members)
}

func TestMemberAliasesRejectMalformedCollectionItems(t *testing.T) {
	for _, aliases := range []string{`{}`, `{"ko":null,"ja":[]}`, `{"ko":[null],"ja":[]}`, `{"ko":[],"ja":[1]}`} {
		member := strings.TrimSuffix(memberFixture, "}") + `,"aliases":` + aliases + `}`
		_, err := memberFixtureClient(t, `{"status":"ok","members":[`+member+`]}`).GetMembers(t.Context())
		require.Error(t, err)
	}
}

func TestCalendarRejectsMissingAndNullRequiredFields(t *testing.T) {
	for _, body := range []string{
		`null`, `{"status":"ok","month":9,"year":2026,"entries":null}`,
		`{"status":"ok","month":9,"year":2026,"entries":[null]}`,
		`{"status":"ok","month":9,"year":2026,"entries":[{"kind":"birthday","day":9,"member":null}]}`,
		`{"status":"ok","month":9,"year":2026,"entries":[{"kind":"birthday","member":` + memberFixture + `}]}`,
	} {
		_, err := memberFixtureClient(t, body).GetCalendar(t.Context(), CalendarQuery{})
		require.Error(t, err)
	}
}

func TestCalendarRangeRefusalBeforeUpstreamIO(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusOK) }))
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL, "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	for _, value := range []int{0, 13} {
		_, err := client.GetCalendar(t.Context(), CalendarQuery{Month: &value})
		require.Error(t, err)
	}

	for _, value := range []int{1999, 2101} {
		_, err := client.GetCalendar(t.Context(), CalendarQuery{Year: &value})
		require.Error(t, err)
	}

	require.Zero(t, calls.Load())
}

func TestContractResponseFixtures(t *testing.T) {
	members, err := memberFixtureClient(t, `{"status":"ok","members":[`+memberFixture+`]}`).GetMembers(t.Context())
	require.NoError(t, err)

	calendar, err := memberFixtureClient(t, `{"status":"ok","month":9,"year":2026,"entries":[{"kind":"birthday","day":9,"ordinal":0,"member":`+memberFixture+`}]}`).GetCalendar(t.Context(), CalendarQuery{})
	require.NoError(t, err)

	fixtures := otherReadFixtures(t)
	maps.Copy(fixtures, mutationFixtures(t))

	fixtures["MembersResponse"] = []MembersResponse{members}
	fixtures["CalendarResponse"] = []CalendarResponse{calendar}

	data, err := jsonv2.Marshal(fixtures)
	require.NoError(t, err)
	t.Logf("CONTRACT_FIXTURES %s", data)
}

func decodedMemberList(t *testing.T, response MembersResponse) []contract.Member {
	t.Helper()

	data, err := jsonv2.Marshal(response)
	require.NoError(t, err)

	var body struct {
		Status  string            `json:"status"`
		Members []contract.Member `json:"members"`
	}

	require.NoError(t, jsonv2.Unmarshal(data, &body))
	require.Equal(t, "ok", body.Status)

	return body.Members
}
