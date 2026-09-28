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

package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/envutil"
	"github.com/park285/shared-go/v2/pkg/promptguard"

	llmclient "github.com/kapu/hololive-api/internal/planes/llm/internal/llm"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/consensus"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	mnsummarizer "github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/summarizer"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

// initMemberNewsService는 member news runtime을 조립한다. 활성인 LLM client나 설정된 X allowlist를 초기화하지
// 못하면 조용히 기능을 줄이지 않고 오류를 돌려 기동을 실패시킨다(stack audit B5).
func initMemberNewsService(
	ctx context.Context,
	provider settings.LLMProviderConfig,
	llmConfig *settings.LLMConfig,
	exaConfig settings.ExaConfig,
	postgres database.Client,
	membersData domain.MemberDataProvider, guards *llmGuards,
	logger *slog.Logger,
) (*membernews.Service, error) {
	if llmConfig == nil {
		llmConfig = &settings.LLMConfig{}
	}

	clients, err := provideMemberNewsLLMClients(provider, llmConfig, ProvideLLMCostTracker(), logger)
	if err != nil {
		return nil, fmt.Errorf("provide member news LLM clients: %w", err)
	}

	validator, err := initMemberNewsSourceValidator(membersData, logger)
	if err != nil {
		return nil, fmt.Errorf("init member news source validator: %w", err)
	}

	repository := membernews.NewRepository(postgres)
	llmClient := guardLLMClient(clients.summary, guards)
	reviewer := guardLLMClient(clients.reviewer, guards)
	adjudicator := guardLLMClient(clients.adjudicator, guards)

	searcher := provideExaSearcher(exaConfig, logger)

	var promptGuard *promptguard.Guard

	if guards != nil {
		promptGuard = guards.prompt
	}

	// 요약 LLM이 꺼져 있으면 summarizer를 두지 않는다. 그때 결정적 digest는 membernews.Service가
	// llm_disabled 사유로 만든다.
	var summarizer model.Summarizer

	if llmClient != nil {
		baseSummarizer := mnsummarizer.NewSummarizer(llmClient, searcher, validator, logger, mnsummarizer.WithPromptGuard(promptGuard))

		summarizer = baseSummarizer

		if llmConfig.MemberNews.Enabled && reviewer != nil {
			summarizer = mnsummarizer.NewConsensusSummarizer(
				baseSummarizer, reviewer, adjudicator, validator,
				consensus.Config{
					ConfidenceThreshold: llmConfig.MemberNews.Confidence,
					ReviewTimeout:       time.Duration(llmConfig.MemberNews.ReviewTimeout) * time.Second,
					AdjudicateTimeout:   time.Duration(llmConfig.MemberNews.AdjudicateTimeout) * time.Second,
				},
				logger,
			)
			logger.Info("Consensus summarizer enabled",
				slog.Float64("confidence_threshold", llmConfig.MemberNews.Confidence),
				slog.Int("review_timeout_sec", llmConfig.MemberNews.ReviewTimeout),
				slog.Int("adjudicate_timeout_sec", llmConfig.MemberNews.AdjudicateTimeout),
			)
		}
	}

	service := membernews.NewService(repository, summarizer, validator, membersData, logger, membernews.WithPromptGuard(promptGuard))
	// 구독 목록 조회는 기동 확인용이다. DB 일시 오류로 기동을 막지 않도록 경고만 남긴다.
	if _, err := service.ListSubscribedRooms(ctx); err != nil {
		logger.Warn("Member news subscription check failed", slog.String("error", err.Error()))
	}

	return service, nil
}

// memberNewsLLMClients는 member news가 쓰는 LLM client 묶음이다. 설정으로 꺼진 기능의 필드는 nil이다.
type memberNewsLLMClients struct {
	summary     llmclient.Client
	reviewer    llmclient.Client
	adjudicator llmclient.Client
}

func provideMemberNewsLLMClients(provider settings.LLMProviderConfig, llmConfig *settings.LLMConfig, tracker llmclient.CostTracker, logger *slog.Logger) (memberNewsLLMClients, error) {
	var clients memberNewsLLMClients

	summary, err := ProvideMemberNewsLLMClient(provider, llmConfig, tracker, logger)
	if err != nil && !isLLMFeatureDisabled(err) {
		return memberNewsLLMClients{}, fmt.Errorf("member news summary client: %w", err)
	}

	clients.summary = summary

	reviewer, err := ProvideMemberNewsReviewerClient(provider, llmConfig, tracker, logger)
	if err != nil && !isLLMFeatureDisabled(err) {
		return memberNewsLLMClients{}, fmt.Errorf("member news reviewer client: %w", err)
	}

	clients.reviewer = reviewer

	adjudicator, err := ProvideMemberNewsAdjudicatorClient(provider, llmConfig, tracker, logger)
	if err != nil && !isLLMFeatureDisabled(err) {
		return memberNewsLLMClients{}, fmt.Errorf("member news adjudicator client: %w", err)
	}

	clients.adjudicator = adjudicator

	return clients, nil
}

func guardLLMClient(client llmclient.Client, guards *llmGuards) llmclient.Client {
	if client == nil {
		return nil
	}

	if guards == nil {
		return llmclient.NewGuardedClient(client, nil)
	}

	return llmclient.NewGuardedClient(client, guards.output)
}

// initMemberNewsSourceValidator는 X allowlist 경로를 MEMBER_NEWS_X_ALLOWLIST_PATH 하나로만 받는다. 값이 없으면
// X allowlist 없이(공식 도메인·YouTube 채널만) 검증한다. 값이 있는데 읽지 못하면 빈 allowlist로 내려가지 않고
// 오류를 돌려 기동을 실패시킨다. 작업 디렉터리 기준 후보 경로 탐색은 두지 않는다(stack audit B5).
func initMemberNewsSourceValidator(membersData domain.MemberDataProvider, logger *slog.Logger) (*membernews.SourceValidator, error) {
	allowlistPath := strings.TrimSpace(envutil.StringRaw("MEMBER_NEWS_X_ALLOWLIST_PATH", ""))

	validator, err := membernews.NewSourceValidator(allowlistPath, membersData, logger)
	if err != nil {
		return nil, fmt.Errorf("load member news x allowlist: %w", err)
	}

	return validator, nil
}
