package scraping

import "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/scraping/backoff"

type BackoffState = backoff.BackoffState

type BackoffOption = backoff.BackoffOption

var NewBackoffState = backoff.NewBackoffState

var WithCooldownJitter = backoff.WithCooldownJitter
