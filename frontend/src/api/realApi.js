import { config } from './config'
import { nameOf } from '../state/users'

async function request(base, path, options = {}) {
  const response = await fetch(`${base}${path}`, { headers: { 'Content-Type': 'application/json' }, ...options })
  const body = await response.json().catch(() => ({}))
  // Our services reply {"error": "Bid too low — ..."}; show that text as-is.
  if (!response.ok) throw new Error(body.error || body.message || `Request failed (${response.status})`)
  return body
}

// The pages were built against mockApi's shapes. These adapters map each
// service's real JSON onto those shapes, so the pages work in both modes.

const fallbackPhoto = 'https://images.unsplash.com/photo-1610701596007-11502861dcfa?auto=format&fit=crop&w=900&q=80'
const titles = {
  started: 'Auction started', outbid: "You've been outbid!", extended: 'Auction extended', won: 'You won!',
  closed: 'Auction closed', unsold: 'Item unsold', receipt: 'Receipt ready', sold: 'Item sold', payment_failed: 'Payment failed',
}

// The route uses the listing id; Bidding is keyed by the auctionId Listing assigns on start.
const auctionIds = new Map()

async function auctionIdFor(listingId) {
  if (!auctionIds.has(listingId)) await listingsApi.get(listingId)
  return auctionIds.get(listingId)
}

// Current bid and bid count live in Bidding; null if it hasn't opened the auction yet.
async function auctionSummary(auctionId) {
  if (!auctionId) return null
  return request(config.bidding, `/auctions/${auctionId}/bids`).catch(() => null)
}

function toItem(listing, summary) {
  const id = String(listing.id)
  if (listing.auctionId) auctionIds.set(id, listing.auctionId)
  return {
    id,
    auctionId: listing.auctionId,
    title: listing.title,
    description: listing.description,
    category: listing.category || 'Handicraft',
    artisanId: listing.artisanId,
    artisan: nameOf(listing.artisanId),
    photoUrl: listing.imageUrl || fallbackPhoto,
    startPrice: listing.startPrice,
    duration: Math.round(listing.durationSeconds / 60),
    status: listing.status,
    endsAt: listing.endsAt,
    currentBid: summary?.highestAmount ?? listing.finalPrice ?? listing.startPrice,
    highestBidder: summary?.highestBidderId ?? listing.winnerId ?? null,
    bids: summary?.bids?.length ?? 0,
  }
}

const toBid = (bid) => ({ id: bid.bidId, bidderId: bid.bidderId, amount: bid.amount, createdAt: bid.placedAt })

const toNote = (note) => ({
  id: note.id, userId: note.userId, type: note.type, title: titles[note.type] || note.type,
  body: note.message, auctionId: note.auctionId, read: note.read, createdAt: note.createdAt,
})

function socket(path, onMessage) {
  let ws
  let stopped = false
  let retry = 500
  const connect = () => {
    ws = new WebSocket(`${config.notificationWs}${path}`)
    ws.onopen = () => { retry = 500 }
    ws.onmessage = (message) => { try { onMessage(JSON.parse(message.data)) } catch { /* Ignore malformed frames. */ } }
    ws.onclose = () => {
      if (stopped) return
      setTimeout(connect, retry)
      retry = Math.min(retry * 2, 10000)
    }
  }
  connect()
  return () => { stopped = true; ws?.close() }
}

const listingsApi = {
  all: async () => {
    const listings = await request(config.listing, '/listings')
    return Promise.all(listings.map(async (listing) =>
      toItem(listing, listing.status === 'Open' ? await auctionSummary(listing.auctionId) : null)))
  },
  get: async (id) => {
    const listing = await request(config.listing, `/listings/${id}`)
    return toItem(listing, await auctionSummary(listing.auctionId))
  },
  create: async (data) => toItem(await request(config.listing, '/listings', {
    method: 'POST',
    body: JSON.stringify({
      title: data.title,
      description: data.description || '',
      imageUrl: data.photoUrl,
      artisanId: data.artisanId,
      category: data.category,
      startPrice: data.startPrice,
      durationSeconds: data.duration * 60,
    }),
  })),
  start: async (id) => toItem(await request(config.listing, `/listings/${id}/start`, { method: 'POST' })),
}

export const realApi = {
  listings: listingsApi,
  bids: {
    all: async (listingId) => {
      const summary = await auctionSummary(await auctionIdFor(listingId))
      return (summary?.bids || []).map(toBid)
    },
    place: async (listingId, data) => {
      const auctionId = await auctionIdFor(listingId)
      if (!auctionId) throw new Error('This auction has not started yet')
      return toBid(await request(config.bidding, `/auctions/${auctionId}/bids`, { method: 'POST', body: JSON.stringify(data) }))
    },
    byBidder: async (bidderId) => (await request(config.bidding, `/bids?bidderId=${encodeURIComponent(bidderId)}`)).map((bid) => ({
      ...toBid(bid), auctionId: bid.auctionId, title: bid.itemTitle, auctionStatus: bid.auctionStatus, isHighest: bid.isHighest,
    })),
  },
  payments: {
    all: async (buyerId) => (await request(config.payment, `/payments?buyerId=${encodeURIComponent(buyerId)}`)).map((payment) => ({
      receiptNo: payment.receiptNo, amount: payment.amount, auction: payment.itemTitle, auctionId: payment.auctionId,
      status: payment.status, reason: payment.reason, createdAt: payment.createdAt,
    })),
    wallet: (buyerId) => request(config.payment, `/wallets/${encodeURIComponent(buyerId)}`),
  },
  notifications: {
    all: async (userId) => ((await request(config.notification, `/notifications?userId=${encodeURIComponent(userId)}`)) || []).map(toNote),
    read: (id) => request(config.notification, `/notifications/${id}/read`, { method: 'PATCH' }),
  },
  health: (base = config.listing) => request(base, '/health'),
  // Read-only queue counts, proxied by the frontend's nginx (see nginx.conf).
  queues: () => request('', '/mq/queues'),
  // Demo controls (control service, host laptop only).
  control: {
    services: () => request(config.control, '/services'),
    stop: (name) => request(config.control, `/services/${name}/stop`, { method: 'POST' }),
    start: (name) => request(config.control, `/services/${name}/start`, { method: 'POST' }),
    // Sends the envelope unchanged, so its eventId is the same (duplicate test).
    resend: (envelope) => request(config.control, '/events/resend', { method: 'POST', body: JSON.stringify(envelope) }),
    poison: () => request(config.control, '/events/poison', { method: 'POST' }),
  },
  // Raw event stream for the admin feed and live room updates (replays the last 50 on connect).
  subscribe: (listener) => socket('/ws/events', listener),
  // This user's notifications as Notification stores them, pushed live.
  subscribeUser: (userId, listener) => socket(`/ws?userId=${encodeURIComponent(userId)}`, (note) => listener(toNote(note))),
}
