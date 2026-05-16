package youtube

import (
	"log/slog"

	oauthservice "github.com/kapu/hololive-shared/pkg/service/youtube/internal/oauthservice"
)

type OAuthService = oauthservice.OAuthService

func NewYouTubeOAuthService(logger *slog.Logger) (*OAuthService, error) {
	return oauthservice.NewYouTubeOAuthService(logger)
}
