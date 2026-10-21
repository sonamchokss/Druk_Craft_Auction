# Notification Service

Consumes `auction.events` through RabbitMQ, creates user notifications in PostgreSQL, and broadcasts them over WebSockets. It listens on port `8005`.

## Run

```bash
export RABBITMQ_URL=amqp://guest:guest@localhost:5672/
export DATABASE_URL=postgres://postgres:postgres@localhost:5432/notifications_db?sslmode=disable
export PORT=8005
go mod download
go test ./...
go run ./cmd/notification
```

Startup retries PostgreSQL and RabbitMQ connections. RabbitMQ declares the durable `auction.events` topic exchange, `auction.dlx`, `notification.queue`, and `notification.dlq` idempotently. Messages use manual acknowledgements and prefetch 10.

## API

```bash
curl http://localhost:8005/health
curl 'http://localhost:8005/notifications?userId=U-7'
curl 'http://localhost:8005/notifications?userId=U-7&unread=true'
curl -X PATCH http://localhost:8005/notifications/1/read
wscat -c 'ws://localhost:8005/ws?userId=U-7'
wscat -c 'ws://localhost:8005/ws/events'
```

The browser origin `http://localhost:3000` is allowed by CORS. Event sockets replay the most recent 50 raw envelopes on connect.

## Test publishing

`python publisher.py BidPlaced`, `AuctionExtended`, `AuctionClosed`, or `PaymentCompleted` prints an envelope ready for the RabbitMQ management UI Publish Message form. Publish to exchange `auction.events`; use routing keys `bid.placed`, `auction.extended`, `auction.closed`, and `payment.completed`. Re-publish the same JSON to test `duplicate-skipped`.

To test malformed-message retries, publish `{bad json` with any routing key. It is retried three times and then dead-lettered to `notification.dlq`. Every message produces a one-line structured result log.

## Compose service snippet

```yaml
notification:
  build: ./notification
  ports: ["8005:8005"]
  environment:
    RABBITMQ_URL: amqp://guest:guest@rabbitmq:5672/
    DATABASE_URL: postgres://postgres:postgres@postgres:5432/notifications_db?sslmode=disable
    PORT: "8005"
  depends_on:
    rabbitmq:
      condition: service_healthy
    postgres:
      condition: service_healthy
  restart: unless-stopped
```
