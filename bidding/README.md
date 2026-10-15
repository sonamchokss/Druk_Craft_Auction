# Bidding service — :8002

Owner: Pema Losel Maurer. Database: `bids_db`. Queue: `bidding.queue`.

## REST API

### `POST /auctions/{id}/bids`

```json
{ "bidderId": "U-7", "amount": 4500 }
```

`201 Created`:

```json
{ "bidId": "B-1002", "auctionId": "A-102", "bidderId": "U-7", "amount": 4500, "placedAt": "2026-10-20T10:15:30Z" }
```

Rejections are `{"error": "..."}`. Show the message to the user as-is. **A rejected bid publishes no event.**

| Status | When | Example message |
|---|---|---|
| 400 | Missing `bidderId`, amount ≤ 0, or not a whole number | `Invalid bid — amount must be a whole number of Ngultrum` |
| 403 | Artisan bidding on their own item | `You can't bid on your own item` |
| 404 | Unknown auction, or `AuctionStarted` not received yet | `Auction not found or not open yet` |
| 409 | Auction closed | `Auction is closed — bids are no longer accepted` |
| 409 | Bidder is already the highest bidder | `You are already the highest bidder` |
| 422 | Below start price, or ≤ current highest | `Bid too low — the current highest bid is Nu. 4200` |

### `GET /auctions/{id}/bids`

Auction summary and bid history, highest first. This is the live auction room's initial load:

```json
{
  "auctionId": "A-102", "itemTitle": "Green Tara Thangka", "status": "open",
  "startPrice": 3000, "endsAt": "2026-10-20T10:20:00Z",
  "highestAmount": 4500, "highestBidderId": "U-7", "winnerId": null,
  "bids": [ { "bidId": "B-1002", "bidderId": "U-7", "amount": 4500, "placedAt": "..." } ]
}
```

`endsAt` is the *original* end time. Take live countdown updates from `AuctionExtended` via Notification's WebSocket.

### `GET /bids?bidderId=U-7`

A buyer's bids across all auctions, for the buyer page's "My bids". Each entry adds `itemTitle`, `auctionStatus` and `isHighest`.

## Events

| Direction | Event | Effect |
|---|---|---|
| consumes | `AuctionStarted` | Creates the auction locally as `open` with its start price |
| consumes | `AuctionClosed` | Marks the auction `closed`; further bids get 409 |
| publishes | `BidPlaced` | After each accepted bid, with `previousBidderId`/`previousAmount` (`null` on the first bid) |

## How it stays correct

- **Concurrent bids:** each bid locks the auction row (`SELECT … FOR UPDATE`). In a race of equal bids, exactly one wins and the rest get 422.
- **No lost `BidPlaced`:** the event is written to an `outbox` table in the same transaction as the bid. A relay publishes it immediately, and retries every 2 s if RabbitMQ is down. Delivery is at-least-once, and consumers deduplicate by `eventId`.
- **Duplicates:** consumed `eventId`s go into `processed_events` in the same transaction as the change. A redelivery is logged as `duplicate-skipped`.
- **Poison messages:** retried 3 times (the `x-retry-count` header, re-sent only to `bidding.queue`), then nacked into `bidding.dlq`.
