const photo = {
  mask: 'https://images.unsplash.com/photo-1577083552431-6e5fd01988f5?auto=format&fit=crop&w=900&q=80',
  textile: 'https://images.unsplash.com/photo-1590736969955-71cc94901144?auto=format&fit=crop&w=900&q=80',
  bowl: 'https://images.unsplash.com/photo-1610701596007-11502861dcfa?auto=format&fit=crop&w=900&q=80',
  thangka: 'https://images.unsplash.com/photo-1577083288073-40892c0860a4?auto=format&fit=crop&w=900&q=80',
}

const now = Date.now()
let listings = [
  { id: 'lot-101', title: 'Crimson Guardian Mask', category: 'Masks', artisanId: 'A-1', artisan: 'Pema Dorji', photoUrl: photo.mask, startPrice: 1800, currentBid: 6400, highestBidder: 'U-7', status: 'Open', endsAt: new Date(now + 112000).toISOString(), bids: 12 },
  { id: 'lot-102', title: 'Handwoven Yathra Kira', category: 'Textiles', artisanId: 'A-1', artisan: 'Karma Wangmo', photoUrl: photo.textile, startPrice: 4200, currentBid: 8700, highestBidder: 'U-3', status: 'Open', endsAt: new Date(now + 278000).toISOString(), bids: 8 },
  { id: 'lot-103', title: 'Carved Juniper Offering Bowl', category: 'Woodwork', artisanId: 'A-1', artisan: 'Sonam Choden', photoUrl: photo.bowl, startPrice: 1200, currentBid: 3900, highestBidder: 'U-9', status: 'Open', endsAt: new Date(now + 421000).toISOString(), bids: 15 },
  { id: 'lot-104', title: 'Wheel of Life Thangka', category: 'Thangkas', artisanId: 'A-1', artisan: 'Tashi Lhamo', photoUrl: photo.thangka, startPrice: 9500, currentBid: 14800, highestBidder: 'U-7', status: 'Open', endsAt: new Date(now + 654000).toISOString(), bids: 21 },
]
let bidMap = Object.fromEntries(listings.map((item) => [item.id, [
  { id: 'b-1', bidderId: item.highestBidder, amount: item.currentBid, createdAt: new Date(now - 55000).toISOString() },
  { id: 'b-2', bidderId: 'U-3', amount: item.currentBid - 600, createdAt: new Date(now - 108000).toISOString() },
]]))
let notifications = [
  { id: 'n-1', userId: 'U-3', type: 'extended', title: 'Auction extended', body: 'Yathra Kira has two extra minutes.', read: false, createdAt: new Date(now - 180000).toISOString() },
  { id: 'n-2', userId: 'U-3', type: 'receipt', title: 'Receipt ready', body: 'Your receipt for Lotus Offering Bowl is ready.', read: true, createdAt: new Date(now - 420000).toISOString() },
]
const listeners = new Set()
const wait = (value) => new Promise((resolve) => setTimeout(() => resolve(structuredClone(value)), 180))
const emit = (event) => { listeners.forEach((listener) => listener(event)) }
const event = (eventType, data) => ({ eventId: `evt-${Date.now()}`, eventType, occurredAt: new Date().toISOString(), source: 'mock', data })

export const mockApi = {
  listings: {
    all: () => wait(listings),
    get: (id) => wait(listings.find((item) => item.id === id)),
    create: async (data) => { const item = { ...data, id: `lot-${Date.now()}`, category: data.category || 'New listing', artisan: 'You', artisanId: 'A-1', currentBid: data.startPrice, status: 'Draft', bids: 0 }; listings = [item, ...listings]; return wait(item) },
    start: async (id) => { const item = listings.find((entry) => entry.id === id); item.status = 'Open'; item.endsAt = new Date(Date.now() + item.duration * 60000).toISOString(); emit(event('AuctionStarted', { auctionId: id })); return wait(item) },
  },
  bids: {
    all: (id) => wait(bidMap[id] || []),
    place: async (id, data) => {
      const item = listings.find((entry) => entry.id === id)
      if (item.status !== 'Open' || new Date(item.endsAt) <= new Date()) throw new Error('Auction closed')
      if (data.amount <= item.currentBid) throw new Error('Bid too low')
      const previous = { bidderId: item.highestBidder, amount: item.currentBid }
      item.currentBid = data.amount; item.highestBidder = data.bidderId; item.bids += 1
      const bid = { id: `b-${Date.now()}`, bidderId: data.bidderId, amount: data.amount, createdAt: new Date().toISOString() }
      bidMap[id] = [bid, ...(bidMap[id] || [])]
      emit(event('BidPlaced', { auctionId: id, bidId: bid.id, bidderId: data.bidderId, amount: data.amount, previousBidderId: previous.bidderId, previousAmount: previous.amount }))
      if (new Date(item.endsAt).getTime() - Date.now() < 10000) { item.endsAt = new Date(Date.now() + 30000).toISOString(); emit(event('AuctionExtended', { auctionId: id, newEndsAt: item.endsAt })) }
      return wait(bid)
    },
    byBidder: (bidderId) => wait(listings.flatMap((item) => (bidMap[item.id] || [])
      .filter((bid) => bid.bidderId === bidderId)
      .map((bid) => ({ ...bid, auctionId: item.id, title: item.title, auctionStatus: item.status === 'Open' ? 'open' : 'closed', isHighest: item.highestBidder === bidderId && item.currentBid === bid.amount })))),
  },
  payments: {
    all: (buyerId) => wait([{ receiptNo: 'RCP-2026-0041', amount: 3900, auction: 'Carved Juniper Offering Bowl', auctionId: 'lot-103', status: 'Completed' }, { receiptNo: 'RCP-2026-0038', amount: 7200, auction: 'Lotus Offering Bowl', auctionId: 'lot-098', status: 'Completed' }].filter((receipt) => buyerId === 'U-3')),
    wallet: () => wait({ balance: 24500, currency: 'Nu.' }),
  },
  notifications: {
    all: (userId) => wait(notifications.filter((notification) => notification.userId === userId)),
    read: async (id) => { notifications = notifications.map((item) => item.id === id ? { ...item, read: true } : item); return wait(true) },
  },
  health: () => wait({ status: 'ok' }),
  subscribe: (listener) => { listeners.add(listener); return () => listeners.delete(listener) },
  seedNotification: (userId, notification) => { const item = { ...notification, id: `n-${Date.now()}`, userId, read: false, createdAt: new Date().toISOString() }; notifications.unshift(item); emit(event('NotificationCreated', item)) },
  getEvents: () => [{ ...event('AuctionStarted', { auctionId: 'lot-101' }), eventId: 'evt-1001' }, { ...event('BidPlaced', { auctionId: 'lot-103', bidderId: 'U-9', amount: 3900 }), eventId: 'evt-1002' }],
}
