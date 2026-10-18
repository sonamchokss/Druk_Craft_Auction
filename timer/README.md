# Timer service — Sangay Choden

Consumes `AuctionStarted` and `BidPlaced` from `timer.queue`. Timers and the highest bidder survive restarts in `timers_db`. Bids in the final 30 seconds add 30 seconds to the existing deadline and publish `AuctionExtended`. Bids at or after the deadline, lower bids, and bids for closed auctions cannot replace the winner.

Every second, a database transaction locks expired timers, closes each once, and writes `AuctionClosed` to a durable outbox. No bids means `winnerId: null` and `finalPrice: 0`. The relay publishes pending events with the same event IDs after restart. Delivery is at-least-once; consumers must deduplicate.

`GET /health` runs on port 8003 under Compose. Configuration: `DATABASE_URL`, `RABBITMQ_URL`, `PORT`.

Run unit tests with `go test ./timer`. To exercise transactions against a disposable PostgreSQL database, set `TEST_DATABASE_URL` and run `go test -v ./timer`; tests use an isolated temporary schema.

The plan has conflicting anti-sniping examples: the checklist says add 30 seconds, while the demo says reset the countdown to 30 seconds. This implementation follows the checklist and event contract: add 30 seconds to the current deadline.
