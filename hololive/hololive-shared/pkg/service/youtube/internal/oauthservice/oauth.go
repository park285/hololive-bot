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

package oauthservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/park285/shared-go/pkg/json"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

const (
	tokenFile       = "token.json"
	credentialsFile = "credentials.json"
)

type OAuthService struct {
	service *youtube.Service
	config  *oauth2.Config
	token   *oauth2.Token
	logger  *slog.Logger
}

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

func (ys *OAuthService) IsAuthorized() bool {
	return ys != nil && ys.service != nil && ys.token != nil
}

func (ys *OAuthService) GetService() *youtube.Service {
	if ys == nil {
		return nil
	}
	return ys.service
}

func loadToken(file string) (token *oauth2.Token, err error) {
	// #nosec G304 -- token file path is controlled by service configuration.
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("failed to open token file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close token file: %w", closeErr))
		}
	}()

	token = &oauth2.Token{}
	if err = json.NewDecoder(f).Decode(token); err != nil {
		return nil, fmt.Errorf("failed to decode token: %w", err)
	}
	return token, nil
}

func saveToken(file string, token *oauth2.Token) (err error) {
	// #nosec G304 -- token file path is controlled by service configuration.
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open token file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close token file: %w", closeErr))
		}
	}()

	if err := json.NewEncoder(f).Encode(token); err != nil {
		return fmt.Errorf("failed to encode token: %w", err)
	}
	return nil
}
