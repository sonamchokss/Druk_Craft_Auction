# Druk Craft Auction frontend

The React/Vite frontend for the event-driven Bhutanese handicraft auction. The interface is designed to run independently in mock mode for demos, while the API modules mirror the backend contracts for real service integration.

## Run locally

```bash
npm install
npm run dev
```

Mock mode is on by default. To use backend services, create `.env.local` from `.env.example` and set `VITE_USE_MOCK=false`. The app then uses the configured listing, bidding, payment, and notification REST URLs.

## Environment

`VITE_USE_MOCK`, `VITE_LISTING_API` (8001), `VITE_BIDDING_API` (8002), `VITE_PAYMENT_API` (8004), `VITE_NOTIFICATION_API` and `VITE_NOTIFICATION_WS` (8005), and `VITE_RABBIT_MGMT` (15672).

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
- [ ] Place a bid in the last 10 seconds: the countdown resets to 30 seconds and the room flashes.
- [ ] Switch to the previous bidder user: an outbid toast appears instantly.
- [ ] Receive an event: the notification bell badge and dropdown update live.
- [ ] Open `/admin`: the event stream shows color-coded events and expandable JSON.
- [ ] Click “Re-send last event”: a duplicate event appears at the top of the stream.

## Docker

```bash
docker build -t druk-craft-frontend .
docker run --rm -p 3000:3000 druk-craft-frontend
```

The root compose file can use this service snippet:

```yaml
frontend:
  build: ./frontend
  ports:
    - "3000:3000"
  environment:
    VITE_USE_MOCK: "true"
```
