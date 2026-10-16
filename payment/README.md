# Payment service — Sangay Choden

Consumes `AuctionClosed` from `payment.queue`. Auctions without a winner are ignored. Each winning auction creates one invoice and atomically charges the buyer's simulated wallet. Successful payments publish `PaymentCompleted` with a unique receipt number; insufficient balance publishes `PaymentFailed` and leaves the wallet unchanged.

Demo wallets start with Nu. 10,000 on first use. Existing balances are never reset on startup. Money uses integer Ngultrum. Invoice creation, wallet deduction, the processed event ID, and the outgoing event are committed together. Both event ID and auction ID protect against repeat charges. Outbox events retain their IDs across restarts.

- `GET /health` — service health.
- `GET /payments?buyerId=U-7` — invoice array, including status, amount, receiptNo, reason, and createdAt. Missing buyerId returns 400; no invoices returns `[]`.
- `GET /wallets/U-7` — `{"buyerId":"U-7","balance":10000}` before purchases.

Compose exposes port 8004. Configuration: `DATABASE_URL`, `RABBITMQ_URL`, `PORT`.

Run `go test ./payment`. Set `TEST_DATABASE_URL` to a disposable PostgreSQL database and run `go test -v ./payment` for transaction tests. The database user needs CREATE SCHEMA permission; tests drop only their temporary schema.
