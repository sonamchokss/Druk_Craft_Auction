// Services are reached on the same host the page was loaded from, so the same
// build works on the demo laptop (localhost) and on the second laptop
// (http://<host-ip>:3000). VITE_* variables override any of them.
const host = window.location.hostname
const at = (value, port, scheme = 'http') => value || `${scheme}://${host}:${port}`

export const config = {
  // The mock is opt-in, so a build without .env can never silently show fake data.
  useMock: import.meta.env.VITE_USE_MOCK === 'true',
  listing: at(import.meta.env.VITE_LISTING_API, 8001),
  bidding: at(import.meta.env.VITE_BIDDING_API, 8002),
  timer: at(import.meta.env.VITE_TIMER_API, 8003),
  payment: at(import.meta.env.VITE_PAYMENT_API, 8004),
  notification: at(import.meta.env.VITE_NOTIFICATION_API, 8005),
  notificationWs: at(import.meta.env.VITE_NOTIFICATION_WS, 8005, 'ws'),
  rabbit: at(import.meta.env.VITE_RABBIT_MGMT, 15672),
}
