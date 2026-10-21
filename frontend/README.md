# Druk Craft Auction frontend

The React/Vite frontend for the event-driven Bhutanese handicraft auction. It talks to the real services by default; an in-browser mock is available for UI work without the backend.

## Run locally

```bash
npm install
npm run dev
```

The app uses the real services by default. For UI work without the backend, create `.env.local` with `VITE_USE_MOCK=true`.

## Environment

Service URLs default to the host the page was opened from (`http://<host>:8001` … `8005`), so the same build works on `localhost` and on a second laptop at `http://<host-ip>:3000`. Override with `VITE_LISTING_API`, `VITE_BIDDING_API`, `VITE_TIMER_API`, `VITE_PAYMENT_API`, `VITE_NOTIFICATION_API`, `VITE_NOTIFICATION_WS` or `VITE_RABBIT_MGMT` (see `.env.example`). Vite reads these at **build** time.

`src/api/realApi.js` maps each service's JSON onto the shapes the pages use (the same shapes as `mockApi.js`). If a service changes its response, update that file.

## Folder layout

- `src/api/` - real REST adapter, configuration, and the contract-compatible in-browser mock
- `src/components/` - shared shell, header, cards, and feedback
- `src/hooks/` - countdown logic computed from `endsAt`
- `src/pages/` - home, live auction room, artisan, buyer, and admin routes
- `src/state/` - current user, notifications, toast, and event-stream context
- `src/mock/` - reserved extension point for larger fixture sets

## Routes

`/` explore live lots · `/auction/:id` live bidding · `/artisan` maker workspace · `/buyer` collector desk · `/admin` operations control room.

## Manual test checklist

- [ ] Place a valid bid: it succeeds and appears immediately in bid history.
- [ ] Place a bid below the minimum: “Bid too low” is shown without changing the current bid.
- [ ] Place a bid in the last 30 seconds: the end time moves 30 seconds later and the room flashes.
- [ ] As the outbid user (e.g. on a second laptop): an outbid toast appears instantly.
- [ ] Receive an event: the notification bell badge and dropdown update live.
- [ ] Open `/admin`: the event stream shows color-coded events and expandable JSON; queue depth shows real RabbitMQ counts.
- [ ] “Re-send last event” only copies the event in this page's list. For the real duplicate test (T8), re-publish the event from the RabbitMQ management UI.

## Docker

```bash
docker build -t druk-craft-frontend .
docker run --rm -p 3000:3000 druk-craft-frontend
```

In the root `docker-compose.yml` it builds from `./frontend` and serves on port 3000. nginx also proxies `GET /mq/queues` to RabbitMQ for the admin panel's queue counts.
