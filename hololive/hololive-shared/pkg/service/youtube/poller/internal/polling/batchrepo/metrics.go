package batchrepo

import "github.com/kapu/hololive-shared/pkg/domain"

var ObserveOutboxInsert func(kind domain.OutboxKind, result string, count int64)

func observeOutboxInsert(kind domain.OutboxKind, result string, count int64) {
	if ObserveOutboxInsert != nil {
		ObserveOutboxInsert(kind, result, count)
	}
}
