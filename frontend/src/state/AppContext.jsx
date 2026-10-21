import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { api, config } from '../api'

const users = [
  { id: 'U-3', name: 'Tashi Choden', role: 'Buyer', initials: 'TC' },
  { id: 'U-7', name: 'Dorji Wangchuk', role: 'Buyer', initials: 'DW' },
  { id: 'U-9', name: 'Pema Lhamo', role: 'Buyer', initials: 'PL' },
  { id: 'A-1', name: 'Karma Artisans', role: 'Artisan', initials: 'KA' },
]
const AppContext = createContext(null)
export function AppProvider({ children }) {
  const [currentUser, setCurrentUser] = useState(() => users.find((user) => user.id === localStorage.getItem('druk-user')) || users[0])
  const [notifications, setNotifications] = useState([])
  const [events, setEvents] = useState(() => (config.useMock ? api.getEvents() : []))
  const [toast, setToast] = useState(null)
  const [liveStatus, setLiveStatus] = useState('live')
  useEffect(() => { localStorage.setItem('druk-user', currentUser.id); api.notifications.all(currentUser.id).then(setNotifications).catch(() => setNotifications([])) }, [currentUser])
  useEffect(() => {
    if (!api.subscribe) { setLiveStatus('live'); return undefined }
    setLiveStatus('live')
    return api.subscribe((incoming) => {
      setEvents((old) => old.some((event) => event.eventId === incoming.eventId) ? old : [incoming, ...old].slice(0, 80))
      if (!isFresh(incoming)) return // the event socket replays recent history on connect
      // In real mode Notification sends outbid alerts itself (see the effect below).
      if (config.useMock && incoming.eventType === 'BidPlaced' && incoming.data.previousBidderId === currentUser.id) {
        const next = { id: `live-${Date.now()}`, userId: currentUser.id, type: 'outbid', title: "You've been outbid!", body: `A new bid of ${money(incoming.data.amount)} was placed.`, read: false, createdAt: new Date().toISOString() }
        setNotifications((old) => [next, ...old])
        setToast({ tone: 'warning', message: next.title })
      }
      if (incoming.eventType === 'AuctionExtended') setToast({ tone: 'info', message: 'Auction extended by 30 seconds' })
    })
  }, [currentUser])
  useEffect(() => {
    if (!api.subscribeUser) return undefined
    return api.subscribeUser(currentUser.id, (note) => {
      setNotifications((old) => old.some((item) => item.id === note.id) ? old : [note, ...old])
      setToast({ tone: note.type === 'outbid' || note.type === 'payment_failed' ? 'warning' : 'info', message: note.title })
    })
  }, [currentUser])
  useEffect(() => { if (!toast) return undefined; const timer = setTimeout(() => setToast(null), 4200); return () => clearTimeout(timer) }, [toast])
  const unread = notifications.filter((item) => !item.read).length
  const value = useMemo(() => ({ users, currentUser, setCurrentUser, notifications, setNotifications, unread, events, setEvents, toast, setToast, liveStatus }), [currentUser, notifications, unread, events, toast, liveStatus])
  return <AppContext.Provider value={value}>{children}</AppContext.Provider>
}
export const useApp = () => useContext(AppContext)
export const isFresh = (event) => Date.now() - new Date(event.occurredAt).getTime() < 15000
export const money = (amount) => `Nu. ${Number(amount || 0).toLocaleString('en-IN')}`
