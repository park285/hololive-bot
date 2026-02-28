package server

import (
	"github.com/gin-gonic/gin"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

// OAuthCallbackHandler: OAuth 프록시 콜백을 처리하여 Deep Link로 리디렉트합니다.
// GET /oauth/callback?code=XXX&state=YYY
// -> hololive-app://callback?code=XXX&state=YYY
func (h *APIHandler) OAuthCallbackHandler(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	errorParam := c.Query("error")
	errorDesc := c.Query("error_description")

	deepLinkURL := sharedserver.BuildOAuthDeepLinkURL(code, state, errorParam, errorDesc)
	htmlResponse := sharedserver.BuildOAuthRedirectHTML(deepLinkURL, errorParam != "")

	c.Data(200, "text/html; charset=utf-8", []byte(htmlResponse))
}
