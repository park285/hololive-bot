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

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	sharedlogging "github.com/park285/shared-go/pkg/logging"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/app"
)

func main() {
	logger := sharedlogging.NewLogger()

	log.Println("=== PostgreSQL Member Data Integration Test ===")
	log.Println()

	postgresConfig := postgresConfigFromEnv()
	buildCtx, buildCancel := context.WithTimeout(context.Background(), constants.AppTimeout.Build)
	runtime, err := app.BuildDBIntegrationRuntime(buildCtx, &postgresConfig, logger)
	buildCancel()

	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}

	defer runtime.Close()

	log.Println("PostgreSQL connected")

	memberCount, channelIDCount := runIntegrationChecks(context.Background(), runtime)

	log.Println()
	log.Println("=== ALL TESTS PASSED ===")
	log.Println()
	printSummary(memberCount, channelIDCount)
}

func postgresConfigFromEnv() config.PostgresConfig {
	return config.PostgresConfig{
		Host:     envOrDefault("POSTGRES_HOST", constants.DatabaseDefaults.Host),
		Port:     envOrDefaultInt("POSTGRES_PORT", constants.DatabaseDefaults.Port),
		User:     envOrDefault("POSTGRES_USER", constants.DatabaseDefaults.User),
		Password: envOrDefault("POSTGRES_PASSWORD", constants.DatabaseDefaults.Password),
		Database: envOrDefault("POSTGRES_DB", constants.DatabaseDefaults.Database),
	}
}

func runIntegrationChecks(ctx context.Context, runtime *app.DBIntegrationRuntime) (memberCount, channelIDCount int) {
	testChannelID := "UChAnqc_AY5_I3Px5dig3X1Q" // Korone

	memberCount = runRepositoryChecks(ctx, runtime, testChannelID)
	runCacheCheck(ctx, runtime, testChannelID)
	channelIDCount = runAdapterChecks(ctx, runtime, testChannelID)

	return memberCount, channelIDCount
}

func runRepositoryChecks(ctx context.Context, runtime *app.DBIntegrationRuntime, testChannelID string) int {
	repository := runtime.Repository

	log.Println("Repository created")

	members, err := repository.GetAllMembers(ctx)
	if err != nil {
		log.Fatalf("Failed to get all members: %v", err)
	}

	log.Printf("Loaded %d members from PostgreSQL", len(members))

	foundMember, err := repository.FindByChannelID(ctx, testChannelID)
	if err != nil {
		log.Fatalf("Failed to find by channel ID: %v", err)
	}

	if foundMember == nil {
		log.Fatal("Korone not found")
		return 0
	}

	log.Printf("Find by channel ID: %s (aliases: ko=%d, ja=%d)",
		foundMember.Name, len(foundMember.Aliases.Ko), len(foundMember.Aliases.Ja))

	foundMember, err = repository.FindByAlias(ctx, "코로네")
	if err != nil {
		log.Fatalf("Failed to find by alias: %v", err)
	}

	if foundMember == nil {
		log.Fatal("Alias '코로네' not found")
		return 0
	}

	log.Printf("Find by alias '코로네': %s", foundMember.Name)

	return len(members)
}

func runCacheCheck(ctx context.Context, runtime *app.DBIntegrationRuntime, testChannelID string) {
	memberCache := runtime.Cache

	log.Println("Cache created with warm-up")

	foundMember, err := memberCache.GetByChannelID(ctx, testChannelID)
	if err != nil {
		log.Fatalf("Cache GetByChannelID failed: %v", err)
	}

	if foundMember == nil {
		log.Fatal("Korone not in cache")
		return
	}

	log.Printf("Cache hit: %s", foundMember.Name)
}

func runAdapterChecks(ctx context.Context, runtime *app.DBIntegrationRuntime, testChannelID string) int {
	adapter := runtime.MemberAdapter
	adapterCtx := adapter.WithContext(ctx)

	foundMember := adapterCtx.FindMemberByChannelID(testChannelID)
	if foundMember == nil {
		log.Fatal("Adapter failed")
		return 0
	}

	log.Printf("Adapter works: %s", foundMember.Name)

	channelIDs := adapterCtx.GetChannelIDs()
	log.Printf("Adapter GetChannelIDs: %d channels", len(channelIDs))

	allMembers := adapterCtx.GetAllMembers()
	log.Printf("Adapter GetAllMembers: %d members", len(allMembers))

	return len(channelIDs)
}

func printSummary(memberCount, channelIDCount int) {
	fmt.Println("Summary:")
	fmt.Printf("- Total members: %d\n", memberCount)
	fmt.Printf("- With channel ID: %d\n", channelIDCount)
	fmt.Println("- Repository: OK")
	fmt.Println("- Cache: OK")
	fmt.Println("- Adapter: OK")
	fmt.Println("- Alias search: OK")
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		log.Printf("⚠ Invalid integer environment value for %s, using default %d\n", key, fallback)
		return fallback
	}

	return parsed
}
