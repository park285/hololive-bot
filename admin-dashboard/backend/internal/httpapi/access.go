package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kapu/admin-dashboard/internal/contract"
)

type access string

const (
	publicMetadata   access = "public_metadata"
	publicLogin      access = "public_login"
	sessionAccess    access = "session"
	sessionCSRF      access = "session_csrf"
	businessMutation access = "session_csrf_audit"
)

type endpoint struct {
	method  string
	path    string
	id      string
	access  access
	handler gin.HandlerFunc
}

// 명시한 method/path/접근/handler를 생성 inventory와 독립 대조합니다.
func (r *API) endpoints() []endpoint {
	return []endpoint{
		{http.MethodGet, "/admin/meta.json", "getAdminMetadata", publicMetadata, handleMetadata},
		{http.MethodPost, "/admin/api/auth/login", "handle_login", publicLogin, r.handleLogin},
		{http.MethodGet, "/admin/api/auth/session", "handle_session_status", sessionAccess, r.handleSessionStatus},
		{http.MethodPost, "/admin/api/auth/logout", "handle_logout", sessionCSRF, r.handleLogout},
		{http.MethodPost, "/admin/api/auth/heartbeat", "handle_heartbeat", sessionCSRF, r.handleHeartbeat},
		{http.MethodGet, "/admin/api/openapi.json", "getAdminOpenAPI", sessionAccess, r.handleOpenAPI},
		{http.MethodGet, "/admin/api/status", "handle_aggregated_status", sessionAccess, r.handleAggregatedStatus},
		{http.MethodGet, "/admin/api/docker/health", "handle_docker_health", sessionAccess, r.handleDockerHealth},
		{http.MethodGet, "/admin/api/docker/containers", "handle_docker_containers", sessionAccess, r.handleDockerContainers},
		{http.MethodPost, "/admin/api/docker/containers/{name}/restart", "handle_docker_restart", businessMutation, r.handleDockerRestart},
		{http.MethodPost, "/admin/api/docker/containers/{name}/stop", "handle_docker_stop", businessMutation, r.handleDockerStop},
		{http.MethodPost, "/admin/api/docker/containers/{name}/start", "handle_docker_start", businessMutation, r.handleDockerStart},
		{http.MethodGet, "/admin/api/holo/alarms", "holoGetAlarms", sessionAccess, holoRead(r.holo.GetAlarms)},
		{http.MethodGet, "/admin/api/holo/members", "holoGetMembers", sessionAccess, holoRead(r.holo.GetMembers)},
		{http.MethodGet, "/admin/api/holo/rooms", "holoGetRooms", sessionAccess, holoRead(r.holo.GetRooms)},
		{http.MethodGet, "/admin/api/holo/rooms/joined", "holoGetRoomsJoined", sessionAccess, holoRead(r.holo.GetJoinedRooms)},
		{http.MethodGet, "/admin/api/holo/settings", "holoGetSettings", sessionAccess, holoRead(r.holo.GetSettings)},
		{http.MethodGet, "/admin/api/holo/stats", "holoGetStats", sessionAccess, holoRead(r.holo.GetStats)},
		{http.MethodGet, "/admin/api/holo/stats/youtube/community-shorts", "holoGetYouTubeCommunityShortsOps", sessionAccess, holoRead(r.holo.GetYouTubeCommunityShortsOps)},
		{http.MethodGet, "/admin/api/holo/streams/live", "holoGetLiveStreams", sessionAccess, holoStreamRead(r.holo.GetLiveStreams)},
		{http.MethodGet, "/admin/api/holo/streams/upcoming", "holoGetUpcomingStreams", sessionAccess, holoStreamRead(r.holo.GetUpcomingStreams)},
		{http.MethodGet, "/admin/api/holo/members/calendar", "holoGetCalendar", sessionAccess, r.handleHoloCalendar},
		{http.MethodDelete, "/admin/api/holo/alarms", "holoDeleteAlarm", businessMutation, holoMutation(r.holo.DeleteAlarm)},
		{http.MethodPost, "/admin/api/holo/members", "holoAddMember", businessMutation, holoMutation(r.holo.AddMember)},
		{http.MethodPost, "/admin/api/holo/members/{id}/aliases", "holoAddAlias", businessMutation, holoMemberMutation(r.holo.AddAlias)},
		{http.MethodDelete, "/admin/api/holo/members/{id}/aliases", "holoRemoveAlias", businessMutation, holoMemberMutation(r.holo.RemoveAlias)},
		{http.MethodPatch, "/admin/api/holo/members/{id}/graduation", "holoSetGraduation", businessMutation, holoMemberMutation(r.holo.SetGraduation)},
		{http.MethodPatch, "/admin/api/holo/members/{id}/channel", "holoUpdateChannel", businessMutation, holoMemberMutation(r.holo.UpdateChannel)},
		{http.MethodPatch, "/admin/api/holo/members/{id}/name", "holoUpdateMemberName", businessMutation, holoMemberMutation(r.holo.UpdateMemberName)},
		{http.MethodPost, "/admin/api/holo/rooms", "holoAddRoom", businessMutation, holoMutation(r.holo.AddRoom)},
		{http.MethodDelete, "/admin/api/holo/rooms", "holoRemoveRoom", businessMutation, holoMutation(r.holo.RemoveRoom)},
		{http.MethodPost, "/admin/api/holo/rooms/acl", "holoSetAcl", businessMutation, holoMutation(r.holo.SetACL)},
		{http.MethodPost, "/admin/api/holo/settings", "holoUpdateSettings", businessMutation, holoMutation(r.holo.UpdateSettings)},
		{http.MethodPost, "/admin/api/holo/names/room", "holoSetRoomName", businessMutation, holoMutation(r.holo.SetRoomName)},
		{http.MethodPost, "/admin/api/holo/names/user", "holoSetUserName", businessMutation, holoMutation(r.holo.SetUserName)},
	}
}

func validateEndpoints(endpoints []endpoint) error {
	remaining := make(map[string]contract.Operation)

	for _, operation := range contract.Operations() {
		remaining[operation.ID] = operation
	}

	for _, endpoint := range endpoints {
		operation, ok := remaining[endpoint.id]
		if !ok || endpoint.handler == nil || operation.Method != endpoint.method || operation.Path != endpoint.path || operation.Access != string(endpoint.access) || operation.Mutation != (endpoint.access == businessMutation) {
			return fmt.Errorf("unclassified or mismatched endpoint: %s %s", endpoint.method, endpoint.path)
		}

		delete(remaining, endpoint.id)
	}

	if len(remaining) != 0 {
		return fmt.Errorf("contract has %d unregistered endpoints", len(remaining))
	}

	return nil
}

func (r *API) accessHandlers(level access) gin.HandlersChain {
	switch level {
	case publicMetadata, publicLogin:
		return nil
	case sessionAccess:
		return gin.HandlersChain{r.auth()}
	case sessionCSRF:
		return gin.HandlersChain{r.auth(), r.csrf()}
	case businessMutation:
		return gin.HandlersChain{r.auth(), r.csrf(), r.auditMutation(), r.claimMutation()}
	default:
		panic("unclassified admin access")
	}
}

func (r *API) registerEndpoints(engine *gin.Engine) {
	endpoints := r.endpoints()
	if err := validateEndpoints(endpoints); err != nil {
		panic(err)
	}

	for _, endpoint := range endpoints {
		chain := r.accessHandlers(endpoint.access)
		if endpoint.access != publicMetadata {
			chain = append(gin.HandlersChain{r.admit(endpoint.id, endpoint.access == businessMutation)}, chain...)
		}

		chain = append(chain, endpoint.handler)

		path := strings.NewReplacer("{id}", ":id", "{name}", ":name").Replace(endpoint.path)
		engine.Handle(endpoint.method, path, chain...)
	}
}
