# Reliability and failure handling — Sangay Choden

Timer and Payment use `shared/reliability`. Other service owners can adopt `Consume` and `Apply` without changing event contracts. Existing Bidding retains its own consumer and outbox implementation.

`Consume` uses manual acknowledgements. Processing errors are copied directly to the owning queue using the default exchange, preserving the event body and ID and incrementing `x-retry-count`. A separate publisher channel waits for broker confirmation before acknowledging the original. After three retries, `Nack(false, false)` sends the event through the existing dead-letter exchange to that service's DLQ. Successful transactions and duplicate deliveries are acknowledged and logged in the shared format.

`Apply` inserts the event ID into `processed_events` in the same database transaction as the business handler. Errors roll back both. Concurrent deliveries serialize on the unique event ID. Payment also makes auction ID unique, preventing a second charge from a different event ID for the same auction.

Timer and Payment write outgoing events to `reliability_outbox` in the business transaction. The relay sends in sequence and waits for publisher confirmation before marking a row published. A database advisory lock serializes relay replicas. A crash between publishing and marking may redeliver the same ID; this is why consumer deduplication is required. Worker/connection failures exit with a nonzero status so Compose's existing restart policy reconnects and drains durable queues.

## Verification

Run `go test -race ./...` and `go vet ./...`. Database transaction tests require `TEST_DATABASE_URL`; otherwise they explicitly skip. No full-system results or screenshots are claimed by these tests.

## Failure demo and evidence checklist

Start Docker Desktop, then run `docker compose up --build`. For isolated service work use `docker compose up --build timer payment`; Compose starts their infrastructure dependencies. Listing and Notification are still scaffolds in this checkout, so the complete group workflow needs those members' implementations.

1. T2/T5: publish an `AuctionStarted`, followed by `BidPlaced` with `occurredAt` in its final 30 seconds. Check `timers_db.timers` and the `AuctionExtended` event. Compare timestamps: deadline increases by 30 seconds.
2. T3/T4: allow the timer to expire. Check one `AuctionClosed`, one invoice, one wallet deduction, and `PaymentCompleted`. Use a price above the wallet balance to demonstrate `PaymentFailed` instead.
3. T6: `docker compose stop payment`; let another auction close. Capture RabbitMQ management at http://localhost:15672 (`auction` / `auction`) showing the ready message in `payment.queue`.
4. T7: `docker compose start payment`; capture the queue draining, the receipt from `GET http://localhost:8004/payments?buyerId=U-7`, and payment logs.
5. T8: re-publish the exact same `AuctionClosed` JSON (same eventId) through `auction.events` with routing key `auction.closed`. Capture `duplicate-skipped`; compare invoice count and wallet balance before/after.
6. T9: publish malformed JSON to `auction.events` with routing key `auction.closed`. Capture three retry logs, the dead-lettered log, and the message in `payment.dlq`.

Use RabbitMQ management's exchange Publish message form for manual injection. Valid envelope and payload examples are in `docs/events.md`. For persistent demo messages choose delivery mode 2. Capture actual results and screenshots during the live run; the admin UI is another member's assignment.

## Checks performed in this workspace

- `go test -race ./...`: passed (unit tests and existing repository tests).
- `go vet ./...`: passed.
- Timer and Payment PostgreSQL transaction tests: skipped because `TEST_DATABASE_URL` was not set.
- RabbitMQ delivery, stop/start recovery, DLQ routing, and full group workflow: pending a live run; Docker Desktop's Linux engine was unavailable during implementation.
- Screenshots and the lab table's actual results: pending that live run.
