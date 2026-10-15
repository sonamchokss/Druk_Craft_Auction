# Druk Craft Auction — Event Contracts

> **Owner:** Pema Losel Maurer (`feature/event-contracts`).
> If you need to change an event name, routing key or field, open a PR against this file
> **and** `shared/events/events.go`, and tell the group first — every service depends on it.

The Go types for everything below live in [`shared/events`](../shared/events/events.go).
Use them instead of hand-writing JSON so the contract can't drift.

---

## 1. Conventions

| Rule | Value |
|---|---|
| Exchange | `auction.events` — type `topic`, durable |
| Serialisation | JSON, `content-type: application/json` |
| Delivery | persistent (delivery mode 2) |
| IDs | strings (`"A-102"`, `"U-7"`, `"B-5531"`) |
| Money | integer Ngultrum (Nu.), **no decimals** — `4500` means Nu. 4,500 |
| Timestamps | RFC 3339 in UTC, e.g. `"2026-10-20T10:15:30Z"` |
| Field names | `camelCase` |
| AMQP properties | `message_id` = `eventId`, `type` = `eventType` (handy in the RabbitMQ UI) |

## 2. Envelope (every event uses this)

```json
{
  "eventId":    "6f1c2a9e-0b7d-4c1e-9a53-2d7f3e8b1c44",
  "eventType":  "BidPlaced",
  "occurredAt": "2026-10-20T10:15:30Z",
  "source":     "bidding-service",
  "data":       { ... event-specific payload ... }
}
```

| Field | Type | Notes |
|---|---|---|
| `eventId` | UUID string | Unique per event. **Consumers use it for idempotency** (`processed_events` table). Re-publishing the *same* event must keep the *same* `eventId`. |
| `eventType` | string | One of the six names below. |
| `occurredAt` | timestamp | When the business action happened. |
| `source` | string | `listing-service`, `bidding-service`, `timer-service`, `payment-service`. |
| `data` | object | Payload — see section 4. |

## 3. Event summary

| Event | Routing key | Publisher | Consumers |
|---|---|---|---|
| AuctionStarted | `auction.started` | Listing | Bidding, Timer, Notification |
| BidPlaced | `bid.placed` | Bidding | Timer, Notification |
| AuctionExtended | `auction.extended` | Timer | Notification |
| AuctionClosed | `auction.closed` | Timer | Payment, Listing, Bidding, Notification |
| PaymentCompleted | `payment.completed` | Payment | Listing, Notification |
| PaymentFailed | `payment.failed` | Payment | Listing, Notification |

## 4. Payloads

### 4.1 AuctionStarted — `auction.started`

Published by **Listing** when an artisan starts an auction (`POST /listings/{id}/start`).

```json
{
  "auctionId": "A-102",
  "itemId": "I-55",
  "itemTitle": "Hand-painted Thangka — Green Tara",
  "artisanId": "U-1",
  "startPrice": 3000,
  "endsAt": "2026-10-20T10:20:00Z"
}
```

- Bidding opens the auction locally and stores `startPrice`.
- Timer stores `endsAt`.
- `auctionId` is the key everyone uses from now on. Listing generates it.

### 4.2 BidPlaced — `bid.placed`

Published by **Bidding** after a bid passes validation and is saved.

```json
{
  "auctionId": "A-102",
  "bidId": "B-5531",
  "bidderId": "U-7",
  "amount": 4500,
  "previousBidderId": "U-3",
  "previousAmount": 4200
}
```

- `previousBidderId` / `previousAmount` are **`null` on the first bid** of an auction.
- Notification uses `previousBidderId` to send "You've been outbid!" without calling Bidding.
- Timer uses `bidderId`/`amount` to track the current winner and the event time to decide on anti-sniping.
- A rejected bid publishes **nothing** (test T10).

### 4.3 AuctionExtended — `auction.extended`

Published by **Timer** when a bid arrives in the last 30 s (anti-sniping).

```json
{
  "auctionId": "A-102",
  "newEndsAt": "2026-10-20T10:20:30Z"
}
```

### 4.4 AuctionClosed — `auction.closed`

Published by **Timer** when an auction's end time passes.

```json
{
  "auctionId": "A-102",
  "itemTitle": "Hand-painted Thangka — Green Tara",
  "artisanId": "U-1",
  "winnerId": "U-7",
  "finalPrice": 4500
}
```

- **No bids:** `winnerId` is `null` and `finalPrice` is `0`. Listing → `Unsold`; Payment ignores it.
- With a winner: Listing → `Awaiting Payment`, Bidding locks the auction, Payment charges the winner.

### 4.5 PaymentCompleted — `payment.completed`

Published by **Payment** after the winner's wallet is charged.

```json
{
  "auctionId": "A-102",
  "winnerId": "U-7",
  "amount": 4500,
  "receiptNo": "R-20261020-0001"
}
```

### 4.6 PaymentFailed — `payment.failed`

Published by **Payment** when the charge cannot be made.

```json
{
  "auctionId": "A-102",
  "winnerId": "U-7",
  "amount": 4500,
  "reason": "insufficient balance"
}
```

## 5. RabbitMQ topology

All declared by `rabbitmq.DeclareTopology()` in [`shared/rabbitmq`](../shared/rabbitmq/topology.go).
**Every service calls it on startup.** Declaring is idempotent, so every queue already exists
even when its owner is down. That's what lets messages wait in `payment.queue` during the failure demo.

| Queue (durable) | Owner | Bound routing keys | Dead-letter queue |
|---|---|---|---|
| `listing.queue` | Listing | `auction.closed`, `payment.*` | `listing.dlq` |
| `bidding.queue` | Bidding | `auction.started`, `auction.closed` | `bidding.dlq` |
| `timer.queue` | Timer | `auction.started`, `bid.placed` | `timer.dlq` |
| `payment.queue` | Payment | `auction.closed` | `payment.dlq` |
| `notification.queue` | Notification | `#` (all events) | `notification.dlq` |

- Dead-letter exchange: `auction.dlx` (type `direct`, durable).
- Each queue sets `x-dead-letter-exchange = auction.dlx` and
  `x-dead-letter-routing-key = <queue name>`. Each `.dlq` is bound to `auction.dlx` with the queue name,
  so a message rejected from `payment.queue` lands **only** in `payment.dlq`.
- Consumers: manual ack, prefetch = 10.

## 6. Retry and idempotency (agreed contract — implemented in `feature/reliability`)

- Retry count travels in the AMQP header **`x-retry-count`** (integer, starts at 0 / absent).
- On a processing error: if `x-retry-count < 3`, re-publish a copy with the count + 1 and ack the original.
  Otherwise `nack(requeue=false)` → message goes to the service's DLQ.
- Every consumer has:
  ```sql
  CREATE TABLE processed_events (
      event_id     UUID PRIMARY KEY,
      processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
  );
  ```
  Insert the `eventId` **in the same transaction** as the business change. If it already exists, ack and log `duplicate-skipped`.

## 7. Log format

Use `logx.Event(...)` from [`shared/logx`](../shared/logx/logx.go). It writes one JSON line per event:

```json
{"time":"2026-10-20T10:15:30Z","level":"INFO","msg":"event","service":"payment-service",
 "eventType":"AuctionClosed","eventId":"6f1c...","auctionId":"A-102","result":"processed"}
```

`result` is one of: `published`, `processed`, `duplicate-skipped`, `retry`, `dead-lettered`.
