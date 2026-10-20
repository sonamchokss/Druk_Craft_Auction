import { config } from './config'

async function request(base, path, options = {}) {
  const response = await fetch(`${base}${path}`, { headers: { 'Content-Type': 'application/json' }, ...options })
  const body = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(body.message || body.detail || `Request failed (${response.status})`)
  return body
}

export const realApi = {
  listings: {
    all: () => request(config.listing, '/listings'),
    get: (id) => request(config.listing, `/listings/${id}`),
    create: (data) => request(config.listing, '/listings', { method: 'POST', body: JSON.stringify(data) }),
    start: (id) => request(config.listing, `/listings/${id}/start`, { method: 'POST' }),
  },
  bids: {
    all: (id) => request(config.bidding, `/auctions/${id}/bids`),
    place: (id, data) => request(config.bidding, `/auctions/${id}/bids`, { method: 'POST', body: JSON.stringify(data) }),
  },
  payments: {
    all: (buyerId) => request(config.payment, `/payments?buyerId=${buyerId}`),
    wallet: (buyerId) => request(config.payment, `/wallets/${buyerId}`),
  },
  notifications: {
    all: (userId) => request(config.notification, `/notifications?userId=${userId}`),
    read: (id) => request(config.notification, `/notifications/${id}/read`, { method: 'PATCH' }),
  },
  health: (base = config.listing) => request(base, '/health'),
  subscribe: (listener) => {
    let socket
    let stopped = false
    let retry = 500
    const connect = () => {
      socket = new WebSocket(`${config.notificationWs}/ws/events`)
      socket.onopen = () => { retry = 500 }
      socket.onmessage = (message) => { try { listener(JSON.parse(message.data)) } catch { /* Ignore malformed event frames. */ } }
      socket.onclose = () => {
        if (stopped) return
        setTimeout(connect, retry)
        retry = Math.min(retry * 2, 10000)
      }
    }
    connect()
    return () => { stopped = true; socket?.close() }
  },
}
