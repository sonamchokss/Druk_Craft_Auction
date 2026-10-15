# Druk Craft Auction

Event-driven live handicraft auction — WEB303 Lab 6.
Five Go microservices talking through RabbitMQ (`auction.events` topic exchange), each with its own Postgres database.

## Run

```bash
docker compose up --build
```

| What | URL |
|---|---|
| Listing / Bidding / Timer / Payment / Notification | http://localhost:8001 … 8005 (`GET /health`) |
| RabbitMQ management UI | http://localhost:15672 — `auction` / `auction` |
| Postgres | `localhost:5432` — `auction` / `auction` |
| Frontend | http://localhost:3000 (once added) |

Reset all data (DBs + queues): `docker compose down -v`.

## Layout

```
docs/events.md      ← the event contract. Read this first.
shared/
  events/           envelope + payload types for all 6 events
  rabbitmq/         Connect (retry), DeclareTopology, Publisher
  logx/             agreed log format
  db/               Postgres connect (retry) + processed_events DDL
  service/          common bootstrap: DB + RabbitMQ + GET /health + CORS
listing/  bidding/  timer/  payment/  notification/   ← one folder per service
frontend/
db/init/            creates the five databases
```

## Writing your service

Each `main.go` already boots and declares the topology. You add your schema, routes and consumer:

```go
app, ctx, cancel := service.New(events.SourceBidding)
defer cancel()

app.Mux.HandleFunc("POST /auctions/{id}/bids", placeBid)

// publish
app.Publisher.Publish(ctx, events.TypeBidPlaced, events.BidPlaced{...})

// consume
msgs, _ := app.Channel.Consume(rabbitmq.BiddingQueue, "", false, false, false, false, nil)
go func() {
    for m := range msgs {
        env, err := events.Parse(m.Body)
        // ... handle, then m.Ack(false)
    }
}()

app.Run(ctx)
```

The shared retry / DLQ / idempotency helper is coming in `feature/reliability` (Sangay Choden).

## Git workflow

- `main` = demo-ready. `develop` = integration. Work on `feature/<name>` and open a PR into `develop`.
- No direct pushes to `main`/`develop`. Each PR needs one approval.
- Only edit your own service folder. `docs/events.md` and `shared/` go through `feature/event-contracts`.
- Commit messages: `<service>: <what changed>`, e.g. `bidding: validate bid amount`.
