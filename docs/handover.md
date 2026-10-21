# Handover: what everyone should be able to explain

The lab assesses *"the ability of each member to explain and demonstrate their work"* and requires that *"all members understand the overall architecture and communication workflow"* (Part H). During integration, several fixes landed in folders that other people own. This page says what changed in **your** area and why, and gives everyone the same short explanation of the whole system.

Read section 1, then your own section, then `git show <commit>` for each commit listed there.

---

## 1. The whole system in one auction (everyone)

1. **Artisan lists and starts an item.** The UI calls Listing (`POST /listings`, `POST /listings/{id}/start`). Listing saves it in `listings_db` and publishes **AuctionStarted** to the `auction.events` topic exchange.
2. **RabbitMQ fans it out.** The exchange copies the event into every queue bound to `auction.started`: `bidding.queue`, `timer.queue`, `notification.queue`. Each service reads only its own queue and writes only its own database.
3. **A buyer bids.** The UI calls Bidding (`POST /auctions/{id}/bids`). Bidding checks the rules, saves the bid and publishes **BidPlaced**, which carries `previousBidderId`.
   - Notification uses that field to tell the previous leader *"You've been outbid!"* without asking Bidding.
   - Timer: if the bid came in the last 30 s, it publishes **AuctionExtended** (end time +30 s). *Event chaining.*
4. **Time runs out.** Timer checks every second and publishes **AuctionClosed** with the winner. Four services consume it (*multiple consumers*):
   - Listing → *Awaiting Payment*
   - Bidding → locks the auction
   - Payment → charges the winner's wallet
   - Notification → "You won!" / "Item sold"
5. **Payment** publishes **PaymentCompleted** (or **PaymentFailed**). Listing marks the item **Sold**, and Notification sends the receipt.

**Reliability (how failures are handled):**
- Queues are durable and messages persistent, so a stopped service loses nothing; its messages wait (T6/T7).
- Consumers ack manually. On an error they retry 3 times, then the message goes to their own `*.dlq` (T9).
- Every consumer records each `eventId` in `processed_events` in the same transaction as the change, so a redelivered event is skipped (T8).

The exact names, routing keys and payloads are in [events.md](events.md). The test evidence is in [testing.md](testing.md).

---

## 2. Sonam Choki: Listing service, Docker Compose

| Commit | What changed | Why |
|---|---|---|
| `c9b4359` | `x-dead-letter-routing-key` is now `listing.queue` (was `listing.dlq`) | Every service declares every queue. With different arguments RabbitMQ refuses the second declaration (`406 PRECONDITION_FAILED`), so whichever service started second crashed |
| `c9b4359` | `itemId` in AuctionStarted is a string | The contract says IDs are strings. As a number, Bidding, Timer and Notification couldn't decode it, retried 3× and dead-lettered **every** AuctionStarted, so no auction ever opened |
| `e3df5a6` | New `category` column (`ALTER TABLE … ADD COLUMN IF NOT EXISTS`) and request field | The home-page filter (Masks, Textiles…) had nothing to match |
| `dfef69b` | `listing/main_test.go`: 6 tests using go-sqlmock | Listing had no tests; the brief asks that each service can be tested independently |
| compose `b666672`, `c6aba7f`, `f848fa2`, `9ad48a8` | bidding/timer/payment build from the repo root; frontend port `3000:3000`; Postgres and RabbitMQ bound to `127.0.0.1`; new `control` service | `docker compose up --build` failed for the Go services that use `shared/`; nginx listens on 3000; the databases had default passwords open to the network |

**Be ready to explain:** why Listing publishes AuctionStarted *before* committing (the status change rolls back if RabbitMQ is down), and how `handleEvent` makes duplicates harmless (`processed_events` insert in the same transaction).

## 3. Pema Losel Maurer: Bidding, event contracts, RabbitMQ setup, integration fixes

Owned code: `docs/events.md`, `shared/events`, `shared/rabbitmq`, `shared/logx`, `bidding/`. Since the integration phase, also every cross-service fix listed on this page, plus:

| Commit | What |
|---|---|
| `9ad48a8` | `control/`: the service behind the Control room's Stop/Start, Re-send and Send malformed event buttons. It uses the Docker socket, so it's bound to `127.0.0.1`, accepts only the frontend's origins and can only touch the five app containers |
| `b428c2a` | `scripts/integration_test.py` and `docs/testing.md` |

**Be ready to explain:**
- **Topology:** every service declares the whole topology, so `payment.queue` exists even while Payment is down.
- **Dead-lettering:** the dead-letter exchange is `direct`, keyed by queue name, so a poison message lands only in its own service's DLQ.
- **Bidding's outbox:** `BidPlaced` is saved in the same transaction as the bid, then published, so it can't be lost if RabbitMQ blips.
- **Concurrent bids:** a row lock (`SELECT … FOR UPDATE`) stops two simultaneous bids from both winning.

## 4. Sangay Choden: Timer, Payment, reliability helper

No behaviour changes were made in `timer/`, `payment/` or `shared/reliability/` during integration. Your code worked as written, and both services pass T1–T11.

Two things found while testing, worth knowing:
- `TestPaymentTransactions` and `TestTimerTransactions` **skip** unless `TEST_DATABASE_URL` is set. They pass against a real Postgres; see [testing.md](testing.md) for the command. Mention this if asked how you tested.
- The `endsAt` in AuctionStarted has whole seconds, while Listing's REST response has fractions. Timer extends from the event's value, which is correct, but it explains a sub-second difference on screen.

**Be ready to explain:** how Timer decides the winner from the BidPlaced events it saw (it never asks Bidding), and why Payment charges exactly once (the `processed_events` check, T8).

## 5. Sonam Wangmo: Notification service, frontend

**Notification**

| Commit | What changed | Why |
|---|---|---|
| `59e2cc2` | `auction.dlx` declared as `direct` (was `topic`); `notification.queue` sets `x-dead-letter-routing-key`; DLQ bound with the queue name | Same `406` problem as Listing: the consumer reconnected forever and never received an event, while `/health` still said "healthy" |
| `59e2cc2` | Retries go to `notification.queue` via the default exchange, persistent; a failed re-publish requeues | Re-publishing to `auction.events` delivered the event to Payment, Listing and Bidding **again** |
| `59e2cc2` | A `Map()` error is now retried | It was acked as "processed", silently losing the notification |
| `add9d78` | CORS allows any origin | The second laptop loads the UI from `http://<ip>:3000`, which `localhost:3000` blocked |
| `7acbcb4` | `PATCH /notifications/{id}/read` strips `/read` | It used `"10/read"` as the ID, so mark-as-read always returned 404 |
| `8760c13` | `publisher.py` payloads match the contract | `BidPlaced` lacked `bidId`; `PaymentCompleted` had an extra field |

**Frontend** (`400e762`, `9ea74d6`)
- **Real API by default.** The mock is now opt-in (`VITE_USE_MOCK=true`). The Docker image had been showing mock data.
- **Service URLs come from the page's own host**, so the same build works on the second laptop.
- **`src/api/realApi.js` adapts each service's JSON to the shapes the pages already used.** Examples: the listing ID vs the `auctionId` used by Bidding, current bid from Bidding, `{"error": …}` messages, notifications, payments.
- **Number inputs use `step="1"`.** With `step="100"` and `min="1"`, the browser rejected 3000 and blocked low bids before the server could reject them (T10).
- **Live notifications** come from Notification's per-user WebSocket. The event-socket replay no longer duplicates bids or toasts.
- **Buyer page:** real bids, wins and receipts. **Control room:** real queue counts, per-service health and working demo buttons.
- **Artisan:** category picker, real photo upload (shrunk to a small JPEG in the browser), sends `artisanId`.

**Be ready to explain:** how the UI shows changes it didn't cause, i.e. the two WebSockets: `/ws/events` for the live room and admin feed, and `/ws?userId=` for the bell.

---

## 6. Known limitation (say it before you're asked)

There is no login. Every service trusts the user ID the browser sends, so anyone could act as another user. The brief doesn't require authentication, and it was deferred deliberately. The fix would be a signed token issued at login and checked by every service.
