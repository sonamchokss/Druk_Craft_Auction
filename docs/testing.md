# Testing (Lab Part G)

Two layers of tests:

| Layer | What it checks | How to run |
|---|---|---|
| Unit tests | Each service's own rules (bid validation, status changes, idempotency, retry counting, notification mapping) | `go test ./...` in the repo root, in `listing/` and in `notification/`. Set `TEST_DATABASE_URL` to a throwaway Postgres or the payment and timer tests **skip** (see below) |
| Integration tests T1–T11 | The whole system through real RabbitMQ, Postgres and every service | `docker compose up --build`, then `python3 scripts/integration_test.py` |

The integration script prints the table below. It runs real auctions, so it takes about 4 minutes. It also stops and starts the payment service for T6/T7. It watches events through a temporary queue bound to `auction.events` with `#`, so it can check payloads without affecting any service's queue. For the duplicate test (T8) it re-publishes the exact bytes of a captured `AuctionClosed`, so the `eventId` is identical.

## Lab scenarios → our tests

| Part G scenario | Our test |
|---|---|
| Successful business operation | T1 |
| Event delivery | T2 |
| Multiple consumers | T3 |
| Event chaining | T4, T5 |
| Consumer unavailable | T6 |
| Consumer recovery | T7 |
| Duplicate event | T8 |
| Complete user workflow | T11 |
| *(extra)* Processing error / poison message | T9 |
| *(extra)* Invalid input feedback | T10 |

## Integration results

Latest run, 2026-10-21, `develop` with the fixes from `fix/testing`, fresh `docker compose up --build`:

| # | Scenario | Expected result | Actual result | Pass? |
|---|---|---|---|---|
| T1 | Successful operation | Bid saved (201), BidPlaced published | HTTP 201, bid B-1005; BidPlaced amount=3500 previousBidderId=None | ✅ Pass |
| T2 | Event delivery | AuctionStarted processed by Timer (end time stored) and Bidding (auction opened) | Timer logged processed=True; Bidding auction A-3 open=True; endsAt 2026-10-21T10:43:21Z | ✅ Pass |
| T3 | Multiple consumers | AuctionClosed processed by Payment, Listing, Bidding and Notification | AuctionClosed winner=U-T164241a; processed by payment=True, listing=True, bidding=True, notification=True | ✅ Pass |
| T4 | Event chaining | AuctionClosed -> PaymentCompleted -> item marked Sold | PaymentCompleted receipt=R-20261021-0b600bf4-…; listing status=Sold | ✅ Pass |
| T5 | Anti-sniping chain | BidPlaced in the last 30 s -> AuctionExtended, end time +30 s | late bid HTTP 201; AuctionExtended newEndsAt = original + 30 s | ✅ Pass |
| T6 | Consumer unavailable | Payment stopped; message waits in payment.queue; rest of system works | payment stopped; AuctionClosed published=True; payment.queue ready=1; listing status=Awaiting Payment | ✅ Pass |
| T7 | Consumer recovery | Queued message processed after restart, receipt issued | payment started; listing status=Sold; payment.queue ready=0; receipt R-20261021-c4a3999b-… | ✅ Pass |
| T8 | Duplicate event | Logged as duplicate; no second invoice or charge | Payment logged duplicate-skipped=True; invoices 1 -> 1; wallet 6500 -> 6500 | ✅ Pass |
| T9 | Processing error | Retried 3 times, then in payment.dlq | payment logged 3 retries; payment.dlq 1 -> 2 | ✅ Pass |
| T10 | Invalid bid feedback | Rejected with an error message; no event published | HTTP 422 "Bid too low — the current highest bid is Nu. 3500"; BidPlaced events 1 -> 1 | ✅ Pass |
| T11 | Complete workflow | Receipt issued, wallet charged once, item Sold, all notifications delivered, closed auction rejects bids | 1 receipt; wallet 10000 -> 5500; winner notified [extended, outbid, receipt, won]; outbid bidder [extended, outbid]; artisan notified sold; bid after close HTTP 409 | ✅ Pass |

**11 / 11 passed.**

## Unit test results

Same day, all passing. Payment and timer were run against a disposable Postgres (`TEST_DATABASE_URL` set), so none were skipped.

| Module | Test | Covers |
|---|---|---|
| root | `bidding` · `TestValidateBid` | Start price, must beat the current bid, already highest, artisan bidding on own item, closed auction |
| root | `payment` · `TestPaymentTransactions` *(Postgres)* | Winner charged; same `AuctionClosed` again is a duplicate; insufficient balance; unsold auction; invalid amount |
| root | `timer` · `TestExtendedDeadline` | Anti-sniping: only bids in the last 30 s extend, by 30 s |
| root | `timer` · `TestTimerTransactions` *(Postgres)* | Start stores the end time; late bid extends it; duplicate bid skipped; expired auctions closed exactly once |
| root | `shared/logx` · `TestEventLogsServiceOnce` | Log format writes `service` once |
| root | `shared/reliability` · `TestRetryCount` | Reading the `x-retry-count` header |
| `listing/` | `listing` · 6 tests | Status change per event (Awaiting Payment, Unsold, Sold, Payment Failed); duplicate skipped in the same transaction; errors that trigger retry/DLQ; ignored event types; create-listing validation |
| `notification/` | `consumer`, `store` · 4 tests | BidPlaced → outbid; AuctionExtended → known bidders; money format; duplicate creates no notifications |

To run the Postgres tests:

```bash
docker run -d --rm --name test-pg -p 55432:5432 -e POSTGRES_PASSWORD=test postgres:16-alpine
TEST_DATABASE_URL='postgres://postgres:test@localhost:55432/postgres?sslmode=disable' go test ./...
docker stop test-pg
```

## Issues found by testing and fixed

| Found | Fix |
|---|---|
| Listing and Notification declared RabbitMQ queues with different settings; whichever started second failed with `406 PRECONDITION_FAILED` | All declarations match `docs/events.md` §5 |
| Listing sent `itemId` as a number; consumers dead-lettered every `AuctionStarted` | Sent as a string, per the contract |
| Notification re-published retries to `auction.events`, delivering them to every service again | Retries go only to its own queue |
| The UI ran on the mock API in Docker; price/bid inputs rejected valid amounts; listing creation failed (`artisanId` missing) | Real API by default, input fixes, adapter for each service's responses |
| Mark-as-read always returned 404 | Route parsing fixed |

## UI evidence

Screenshots from manual runs of the same scenarios through the frontend are on the `docs/report` branch in `docs/report/screenshots/`:

| Test | Screenshot |
|---|---|
| T1 | `t1-bidplaced-in-queue.png` (RabbitMQ), `ui-outbid-alert.png` |
| T5 | `ui-t5-extended.png` |
| T6 / T7 | `ui-t6-payment-queue-waiting.png`, `ui-t7-recovered.png` |
| T8 | `ui-t8-duplicate.png` |
| T9 | `ui-t9-dlq.png` |
| T10 | `ui-t10-bid-too-low.png` |
| T11 | `ui-t11-sold.png`, `ui-buyer-receipts.png`, `ui-buyer-receipt-detail.png`, `ui-admin.png` |
