import { AlertCircle, ArrowLeft, CheckCircle2, Clock3, Gavel, RefreshCw, Send } from 'lucide-react'
import { Link, useParams } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { api } from '../api'
import { useCountdown } from '../hooks/useCountdown'
import { isFresh, money, useApp } from '../state/AppContext'

// Valid bids always beat the previous one, so highest amount first == newest first.
const addBid = (bids, bid) => bids.some((entry) => entry.id === bid.id) ? bids : [bid, ...bids].sort((a, b) => b.amount - a.amount)

export function AuctionRoom() {
  const { id } = useParams()
  const { currentUser, setToast } = useApp()
  const [item, setItem] = useState(null)
  const [bids, setBids] = useState([])
  const [amount, setAmount] = useState('')
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [extended, setExtended] = useState(false)
  const countdown = useCountdown(item?.endsAt || new Date().toISOString())
  const auctionId = item?.auctionId || id // the mock uses the listing id for both
  const reload = () => api.listings.get(id).then(setItem).catch(() => {})

  useEffect(() => { Promise.all([api.listings.get(id), api.bids.all(id)]).then(([listing, history]) => { setItem(listing); setBids(history) }).catch((err) => setError(err.message)).finally(() => setLoading(false)) }, [id])

  useEffect(() => {
    if (!api.subscribe || !item) return undefined
    return api.subscribe((incoming) => {
      if (incoming.data?.auctionId !== auctionId) return
      if (incoming.eventType === 'BidPlaced') setBids((old) => addBid(old, { id: incoming.data.bidId, bidderId: incoming.data.bidderId, amount: incoming.data.amount, createdAt: incoming.occurredAt }))
      if (incoming.eventType === 'AuctionExtended') {
        setItem((old) => ({ ...old, endsAt: incoming.data.newEndsAt }))
        if (isFresh(incoming)) { setExtended(true); setToast({ tone: 'info', message: 'This auction was extended — you have more time to bid.' }); setTimeout(() => setExtended(false), 1000) }
      }
      if (['AuctionClosed', 'PaymentCompleted', 'PaymentFailed'].includes(incoming.eventType)) reload()
    })
  }, [auctionId, Boolean(item), setToast])

  // Time is up but Listing still says Open: Timer is closing it. Poll until the status changes.
  const awaitingClose = countdown.expired && item?.status === 'Open'
  useEffect(() => { if (!awaitingClose) return undefined; const timer = setInterval(reload, 2000); return () => clearInterval(timer) }, [awaitingClose, id])

  const placeBid = async (event) => { event.preventDefault(); setError(''); setSubmitting(true); try { const bid = await api.bids.place(id, { bidderId: currentUser.id, amount: Number(amount) }); setBids((old) => addBid(old, bid)); setAmount(''); setToast({ tone: 'success', message: 'Your bid is in. Good luck!' }) } catch (err) { setError(err.message) } finally { setSubmitting(false) } }

  if (loading) return <div className="page centered-state">Loading auction…</div>; if (!item || error && !item) return <div className="page centered-state error-state">{error || 'Auction not found'}</div>
  const leading = bids[0]
  const currentBid = leading?.amount ?? item.currentBid
  const highestBidder = leading?.bidderId ?? item.highestBidder
  // First bid may equal the start price; after that it must beat the current highest. The server enforces both.
  const minimum = highestBidder ? currentBid + 1 : item.startPrice
  const closed = item.status !== 'Open' || countdown.expired
  const finalTitle = awaitingClose ? 'Closing auction…' : highestBidder ? 'Auction complete' : 'Auction ended'
  return <div className="page room-page"><Link to="/" className="back-link"><ArrowLeft size={16} /> Back to live auctions</Link><div className="room-layout"><section><div className="room-image"><img src={item.photoUrl} alt={item.title} /><span className="room-category">{item.category}</span></div><div className="room-details"><span className="eyebrow">Lot {item.id.replace('lot-', '#')} · {item.artisan}</span><h1>{item.title}</h1><p className="description">{item.description || 'A one-of-a-kind piece, made with patience and passed-down technique. Every mark carries the character of its maker.'}</p><div className="maker-note"><span className="avatar">✦</span><span>Made in Bhutan by <b>{item.artisan}</b><small>Verified artisan</small></span></div></div></section><aside className="bid-panel"><div className={`countdown ${extended ? 'flash' : ''}`}><Clock3 size={18} /><span>{closed ? 'Auction closed' : 'Auction ends in'} <b aria-live="polite">{closed ? (awaitingClose ? '…' : item.status) : countdown.label}</b></span></div>{closed ? <div className="closed-state"><CheckCircle2 size={38} /><h2>{finalTitle}</h2><p>{highestBidder ? `Winner: ${highestBidder} · ${money(currentBid)}` : 'No winning bid was placed.'}</p></div> : <><div className="current-bid"><small>{highestBidder ? 'Current highest bid' : 'Starting price'}</small><strong>{money(currentBid)}</strong>{highestBidder && <span>by <b>{highestBidder === currentUser.id ? 'You' : highestBidder}</b></span>}</div><form onSubmit={placeBid} className="bid-form"><label htmlFor="bid">Your bid</label><div className="money-input"><span>Nu.</span><input id="bid" type="number" min="1" step="1" value={amount} onChange={(event) => setAmount(event.target.value)} placeholder={`${minimum}`} required /><button disabled={submitting}>{submitting ? <RefreshCw className="spin" size={17} /> : <Send size={17} />}<span>{submitting ? 'Submitting' : 'Place bid'}</span></button></div><small className="hint">Minimum bid: {money(minimum)} · a bid in the last 30 s adds 30 s</small></form>{error && <div className="inline-error"><AlertCircle size={16} /> {error}</div>}</>}<div className="history"><div className="history-head"><h3><Gavel size={16} /> Bid history</h3><span>{bids.length} bids</span></div>{bids.length ? bids.map((bid, index) => <div className={`bid-row ${index === 0 ? 'leading' : ''}`} key={bid.id}><span className="bid-avatar">{bid.bidderId.slice(-1)}</span><span><b>{bid.bidderId === currentUser.id ? 'You' : bid.bidderId}</b><small>{index === 0 ? 'Leading bid' : new Date(bid.createdAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</small></span><strong>{money(bid.amount)}</strong></div>) : <p className="muted">No bids yet. Be the first.</p>}</div></aside></div></div>
}
