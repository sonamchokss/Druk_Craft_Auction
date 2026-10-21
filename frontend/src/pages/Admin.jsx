import { Activity, AlertTriangle, ChevronDown, CircleStop, ExternalLink, HeartPulse, Play, RefreshCw, Server, Wifi } from 'lucide-react'
import { useEffect, useState } from 'react'
import { api, config } from '../api'
import { money, useApp } from '../state/AppContext'

const services = [{ name: 'Listing service', key: 'listing' }, { name: 'Bidding service', key: 'bidding' }, { name: 'Timer service', key: 'timer' }, { name: 'Payment service', key: 'payment' }, { name: 'Notification service', key: 'notification' }]
const queues = ['listing', 'bidding', 'timer', 'payment', 'notification']
// Real counts from RabbitMQ (via nginx); random numbers only in mock mode.
const loadCounts = async () => {
  if (!api.queues) return Object.fromEntries(queues.flatMap((queue) => [[queue, Math.floor(Math.random() * 7)], [`${queue}.dlq`, Math.floor(Math.random() * 2)]]))
  const list = await api.queues()
  const byName = Object.fromEntries(list.map((queue) => [queue.name, queue.messages_ready ?? queue.messages ?? 0]))
  return Object.fromEntries(queues.flatMap((queue) => [[queue, byName[`${queue}.queue`]], [`${queue}.dlq`, byName[`${queue}.dlq`]]]))
}
const noControls = config.useMock ? 'Not available in mock mode' : 'Controls only work on the laptop running docker compose'

export function Admin() {
  const { setToast } = useApp()
  const [events, setEvents] = useState(config.useMock ? api.getEvents() : [])
  const [health, setHealth] = useState({})
  const [containers, setContainers] = useState(null) // null = control service unreachable
  const [expanded, setExpanded] = useState(null)
  const [counts, setCounts] = useState({})
  const [busy, setBusy] = useState('')

  useEffect(() => { if (api.subscribe) return api.subscribe((event) => setEvents((old) => old.some((item) => item.eventId === event.eventId) ? old : [event, ...old])); return undefined }, [])
  const poll = () => {
    Promise.all(services.map(async (service) => [service.key, await api.health(config[service.key]).catch(() => ({ status: 'down' }))])).then((entries) => setHealth(Object.fromEntries(entries)))
    loadCounts().then(setCounts).catch(() => setCounts({}))
    if (api.control) api.control.services().then((list) => setContainers(Object.fromEntries(list.map((item) => [item.name, item.status])))).catch(() => setContainers(null))
  }
  useEffect(() => { poll(); const timer = setInterval(poll, 3000); return () => clearInterval(timer) }, [])

  // Runs a control action with a busy flag and success/error toast.
  const act = async (label, action, success) => {
    setBusy(label)
    try { await action(); setToast({ tone: 'success', message: success }); poll() } catch (err) { setToast({ tone: 'warning', message: err.message }) } finally { setBusy('') }
  }
  const toggle = (key, running) => act(key, () => running ? api.control.stop(key) : api.control.start(key), `${key} ${running ? 'stopped' : 'started'}`)
  // Same envelope, same eventId: consumers should log duplicate-skipped (T8).
  const resend = (event) => act('resend', () => api.control.resend(event), `Re-sent ${event.eventType} (same eventId)`)
  const poison = () => act('poison', () => api.control.poison(), 'Malformed event sent on auction.closed — watch the DLQs')
  const controls = Boolean(api.control) && containers !== null

  return <div className="page admin-page"><div className="page-heading"><div><span className="eyebrow">Operations</span><h1>Control room</h1><p>Observe the auction heartbeat and keep every service moving.</p></div><div className="control-row"><button className="button button-outline" onClick={() => events[0] && resend(events[0])} disabled={!controls || !events.length || busy === 'resend'} title={controls ? 'Re-publish the newest event unchanged (duplicate test)' : noControls}><RefreshCw size={16} className={busy === 'resend' ? 'spin' : ''} /> Re-send last event</button><button className="button button-outline" onClick={poison} disabled={!controls || busy === 'poison'} title={controls ? 'Publish a malformed event (poison message test)' : noControls}><AlertTriangle size={16} /> Send malformed event</button></div></div><div className="admin-grid"><section className="panel event-panel"><div className="panel-heading"><div><h2><Activity size={18} /> Live event stream</h2><p>WebSocket · newest first</p></div><span className="stream-live"><i className="live-dot" /> streaming</span></div><div className="event-list">{events.length ? events.map((item, index) => <div className={`event-row event-${item.eventType.toLowerCase()}`} key={`${item.eventId}-${index}`}><span className="event-line" /><div className="event-main"><div className="event-title"><span className="event-type">{item.eventType}</span><time>{new Date(item.occurredAt).toLocaleTimeString()}</time><button className="icon-button tiny" onClick={() => setExpanded(expanded === index ? null : index)} aria-label="Expand event"><ChevronDown size={14} /></button></div><p>{item.data?.auctionId && `Auction ${item.data.auctionId}`} {item.data?.amount && `· ${money(item.data.amount)}`}</p>{expanded === index && <><pre>{JSON.stringify(item, null, 2)}</pre>{controls && <button className="button button-small" style={{ marginTop: 10 }} onClick={() => resend(item)} disabled={busy === 'resend'}><RefreshCw size={13} /> Re-send this event</button>}</>}</div></div>) : <div className="empty-large"><Wifi size={32} /><p>Waiting for events…</p></div>}</div></section><aside className="admin-side"><section className="panel"><div className="panel-heading"><div><h2><Server size={18} /> Queue depth</h2><p>RabbitMQ management</p></div><a href={config.rabbit} target="_blank" rel="noreferrer" aria-label="Open RabbitMQ management UI"><ExternalLink size={15} /></a></div><div className="queue-list">{queues.map((queue) => <div className="queue-row" key={queue}><span>{queue}</span><b>{counts[queue] ?? '—'}</b><small>ready</small><span className="dlq">{counts[`${queue}.dlq`] ?? '—'} dlq</span></div>)}</div><small className="todo-note">{config.useMock ? 'Mock numbers.' : 'Live from RabbitMQ.'} Full view: <a href={config.rabbit} target="_blank" rel="noreferrer">management UI</a></small></section><section className="panel"><div className="panel-heading"><div><h2><HeartPulse size={18} /> Service health</h2><p>Polled every 3 seconds{controls ? '' : ` · ${noControls}`}</p></div></div><div className="service-list">{services.map((service) => { const up = health[service.key]?.status === 'ok'; const state = containers?.[service.key]; const running = state === 'running'; return <div className="service-row" key={service.key}><i className={up ? 'health-dot' : 'health-dot down'} /><span>{service.name}<small>{up ? 'Healthy' : state && state !== 'running' ? `Container ${state}` : 'Unavailable'}</small></span><button className="service-button" onClick={() => toggle(service.key, false)} disabled={!controls || running || busy === service.key} title={controls ? `Start ${service.key}` : noControls}><Play size={13} /> Start</button><button className="service-button stop" onClick={() => toggle(service.key, true)} disabled={!controls || !running || busy === service.key} title={controls ? `Stop ${service.key}` : noControls}><CircleStop size={13} /> Stop</button></div> })}</div></section></aside></div></div>
}
