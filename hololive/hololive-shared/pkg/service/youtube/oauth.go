package youtube

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

const (
	tokenFile       = "token.json"
	credentialsFile = "credentials.json"
)

// OAuthService: YouTube API OAuth2 인증을 처리하고 관리하는 서비스
type OAuthService struct {
	service *youtube.Service
	config  *oauth2.Config
	token   *oauth2.Token
	logger  *slog.Logger
}

// NewYouTubeOAuthService: 저장된 토큰이나 자격 증명을 로드하여 OAuth 서비스를 초기화합니다.
func NewYouTubeOAuthService(logger *slog.Logger) (*OAuthService, error) {
	if logger == nil {
		logger = slog.Default()
	}

	credBytes, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read credentials file: %w", err)
	}

	config, err := google.ConfigFromJSON(credBytes, youtube.YoutubeReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("unable to parse credentials: %w", err)
	}

	token, err := loadToken(tokenFile)
	if err != nil {
		logger.Warn("No existing token found, need to authorize",
			slog.String("file", tokenFile))

		return &OAuthService{
			config: config,
			token:  nil,
			logger: logger,
		}, nil
	}

	ctx := context.Background()
	client := config.Client(ctx, token)

	ytService, err := youtube.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("failed to create YouTube service: %w", err)
	}

	logger.Info("YouTube OAuth service initialized",
		slog.Bool("authenticated", true))

	return &OAuthService{
		service: ytService,
		config:  config,
		token:   token,
		logger:  logger,
	}, nil
}

// Authorize: CLI 기반의 대화형 OAuth 인증 프로세스를 시작한다. (브라우저 인증 URL 표시 및 코드 입력 대기)
func (ys *OAuthService) Authorize(ctx context.Context) error {
	if ys == nil {
		return fmt.Errorf("service not initialized")
	}

	authURL := ys.config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)

	ys.logger.Info("Authorization required")
	fmt.Println("\n=== YouTube API Authorization ===")         //nolint:forbidigo // CLI interaction output
	fmt.Println("Go to the following link in your browser:")   //nolint:forbidigo // CLI interaction output
	fmt.Println(authURL)                                       //nolint:forbidigo // CLI interaction output
	fmt.Println("\nAfter authorization, enter the code here:") //nolint:forbidigo // CLI interaction output

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		return fmt.Errorf("failed to read auth code: %w", err)
	}

	token, err := ys.config.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("unable to retrieve token: %w", err)
	}

	if saveErr := saveToken(tokenFile, token); saveErr != nil {
		return fmt.Errorf("unable to save token: %w", saveErr)
	}

	ys.token = token

	client := ys.config.Client(ctx, token)
	ytService, err := youtube.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return fmt.Errorf("failed to create YouTube service: %w", err)
	}

	ys.service = ytService

	ys.logger.Info("YouTube OAuth authorization complete",
		slog.String("token_file", tokenFile))

	fmt.Println("\nAuthorization successful. Token saved.") //nolint:forbidigo // CLI interaction output

	return nil
}

// IsAuthorized: 현재 유효한 인증 토큰이 있는지 확인합니다.
func (ys *OAuthService) IsAuthorized() bool {
	return ys != nil && ys.service != nil && ys.token != nil
}

// GetService: 인증된 YouTube API 클라이언트를 반환합니다.
func (ys *OAuthService) GetService() *youtube.Service {
	if ys == nil {
		return nil
	}
	return ys.service
}

func loadToken(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("failed to open token file: %w", err)
	}
	defer f.Close()

	token := &oauth2.Token{}
	if err = json.NewDecoder(f).Decode(token); err != nil {
		return nil, fmt.Errorf("failed to decode token: %w", err)
	}
	return token, nil
}

func saveToken(file string, token *oauth2.Token) error {
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open token file: %w", err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(token); err != nil {
		return fmt.Errorf("failed to encode token: %w", err)
	}
	return nil
}
