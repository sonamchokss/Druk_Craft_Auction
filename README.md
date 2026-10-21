# Druk Craft Auction

Event-driven live handicraft auction — WEB303 Lab 6.
Five Go microservices talking through RabbitMQ (`auction.events` topic exchange), each with its own Postgres database, plus a React frontend.

## Run

```bash
docker compose up --build
```

| What | URL |
|---|---|
| Frontend | http://localhost:3000 |
| Listing / Bidding / Timer / Payment / Notification | http://localhost:8001 … 8005 (`GET /health`) |
| RabbitMQ management UI | http://localhost:15672 — `guest` / `guest` |
| Postgres | `localhost:5432` — `postgres` / `postgres` |

RabbitMQ and Postgres listen on this machine only. Reset all data (DBs + queues): `docker compose down -v`.

### Two-laptop demo

Run the stack on laptop 1. On laptop 2 (same network) open `http://<laptop-1-ip>:3000`. The frontend calls the services on whatever host it was loaded from, so nothing needs configuring. Pick a different buyer in the user menu on each laptop. If laptop 2 can't connect, allow ports 3000 and 8001–8005 through laptop 1's firewall.

## Layout

```
docs/events.md      ← the event contract. Read this first.
docs/reliability.md ← retry / DLQ / idempotency and the failure demo
shared/             ← Go code shared by bidding, timer and payment
  events/           envelope + payload types for all 6 events
  rabbitmq/         Connect (retry), DeclareTopology, Publisher
  reliability/      retries, transactional deduplication, outbox
  logx/             agreed log format
  db/               Postgres connect (retry) + processed_events DDL
  service/          common bootstrap: DB + RabbitMQ + GET /health + CORS
bidding/  timer/  payment/       ← use the root go.mod and shared/
listing/  notification/          ← own go.mod; build from their own folder
frontend/           React + Vite, served by nginx on :3000
db/init/            creates the five databases
```

## Changing RabbitMQ setup

Every service declares the same exchanges and queues. RabbitMQ rejects a declaration whose type or arguments differ from an existing one (`406 PRECONDITION_FAILED`), and whichever service starts second fails. Any queue declaration must match [docs/events.md §5](docs/events.md) exactly:

- `auction.dlx` is a **direct** exchange.
- Each `<service>.queue` sets `x-dead-letter-exchange = auction.dlx` and `x-dead-letter-routing-key = <service>.queue`.
- Each `<service>.dlq` is bound to `auction.dlx` with the key `<service>.queue`.
- Retries go to the service's own queue through the default exchange (`""`), never back to `auction.events`.

## Git workflow

- `main` = demo-ready. `develop` = integration. Work on `feature/<name>` or `fix/<name>` and open a PR into `develop`.
- No direct pushes to `main`/`develop`. Each PR needs one approval.
- Pull `develop` into your branch daily (`git pull origin develop`) so it can merge cleanly.
- Commit messages: `<service>: <what changed>`, e.g. `bidding: validate bid amount`.
