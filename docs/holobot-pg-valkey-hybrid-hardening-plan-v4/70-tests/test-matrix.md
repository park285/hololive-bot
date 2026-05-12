# Test Matrix

## Repository tests

- [ ] InsertBatch empty input.
- [ ] 1 event + 1 delivery.
- [ ] 1 event + 1,000 deliveries.
- [ ] duplicate delivery skip.
- [ ] duplicate terminal row not pending.
- [ ] shadowed row not claimed.
- [ ] same event_key/same hash ok.
- [ ] same event_key/different hash same batch fails.
- [ ] existing event_key/different hash fails.
- [ ] ClaimDue concurrent workers no duplicate.
- [ ] LoadEventsByID distinct IDs.
- [ ] MarkSending ownership mismatch fails.
- [ ] MarkSent ownership mismatch fails.
- [ ] stale leased -> retry.
- [ ] stale sending -> quarantine.

## Publisher tests

- [ ] Publish calls PublishBatch size 1.
- [ ] PublishBatch does not call Publish repeatedly.
- [ ] pg_first commit success + wakeup fail still success.
- [ ] wakeup guard suppresses duplicate tokens.
- [ ] chunk 1 success/chunk 2 fail releases only chunk 2 claims.
- [ ] result metrics include inserted/duplicate/hash conflict.

## Dispatcher tests

- [ ] PG mode Valkey unavailable startup success.
- [ ] PG mode wakeup unavailable fallback scan.
- [ ] valkey mode Valkey unavailable startup fail.
- [ ] group A error does not cancel group B.
- [ ] Iris send failure after MarkSending -> quarantine.
- [ ] render failure before MarkSending -> retry/DLQ.
- [ ] MarkSent failure after send leaves sending for quarantine.

## Valkey command policy tests

- [ ] No KEYS.
- [ ] No PUBLISH in dispatch wakeup.
- [ ] LPUSH one token.
- [ ] BRPOP one key.
- [ ] No unbounded LRANGE.
