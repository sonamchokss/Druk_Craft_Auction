import { ImagePlus, Play, Plus, UploadCloud } from 'lucide-react'
import { useEffect, useState } from 'react'
import { api } from '../api'
import { money, useApp } from '../state/AppContext'
import { categories } from '../state/users'

const defaultPhoto = 'https://images.unsplash.com/photo-1610701596007-11502861dcfa?auto=format&fit=crop&w=900&q=80'
const emptyForm = { title: '', photoUrl: '', category: categories[0], startPrice: '', duration: '5' }

// Listing stores the photo as text, so shrink uploads to a small JPEG data URL
// (≈100 KB) instead of sending a multi-MB original with every listing.
const toPhoto = (file) => new Promise((resolve, reject) => {
  if (!file.type.startsWith('image/')) return reject(new Error('Choose a JPG or PNG image.'))
  if (file.size > 5 * 1024 * 1024) return reject(new Error('Images must be 5MB or smaller.'))
  const url = URL.createObjectURL(file)
  const img = new Image()
  img.onload = () => {
    const scale = Math.min(1, 900 / Math.max(img.width, img.height))
    const canvas = document.createElement('canvas')
    canvas.width = Math.round(img.width * scale)
    canvas.height = Math.round(img.height * scale)
    canvas.getContext('2d').drawImage(img, 0, 0, canvas.width, canvas.height)
    URL.revokeObjectURL(url)
    resolve(canvas.toDataURL('image/jpeg', 0.8))
  }
  img.onerror = () => { URL.revokeObjectURL(url); reject(new Error('That file could not be read as an image.')) }
  img.src = url
})

export function Artisan() { const { currentUser, setToast } = useApp(); const [items, setItems] = useState([]); const [form, setForm] = useState(emptyForm); const [upload, setUpload] = useState(null); const [dragging, setDragging] = useState(false); const [loading, setLoading] = useState(true); const [saving, setSaving] = useState(false); const [error, setError] = useState('')
  const refresh = () => api.listings.all().then((all) => setItems(all.filter((item) => item.artisanId === currentUser.id || currentUser.id === 'A-1'))).catch((err) => setError(err.message)).finally(() => setLoading(false))
  useEffect(refresh, [currentUser.id])
  const change = (event) => setForm({ ...form, [event.target.name]: event.target.value })
  const pick = async (file) => { if (!file) return; setError(''); try { setUpload({ name: file.name, dataUrl: await toPhoto(file) }) } catch (err) { setError(err.message) } }
  const drop = (event) => { event.preventDefault(); setDragging(false); pick(event.dataTransfer.files[0]) }
  const submit = async (event) => { event.preventDefault(); if (!form.title.trim() || Number(form.startPrice) < 1 || Number(form.duration) < 1) { setError('Add a title, a positive start price, and a duration.'); return } setSaving(true); setError(''); try { await api.listings.create({ ...form, artisanId: currentUser.id, startPrice: Number(form.startPrice), duration: Number(form.duration), photoUrl: upload?.dataUrl || form.photoUrl || defaultPhoto }); setForm(emptyForm); setUpload(null); setToast({ tone: 'success', message: 'Draft listing created.' }); refresh() } catch (err) { setError(err.message) } finally { setSaving(false) } }
  const start = async (id) => { try { await api.listings.start(id); setToast({ tone: 'success', message: 'Auction is now live.' }); refresh() } catch (err) { setError(err.message) } }
  return <div className="page"><div className="page-heading"><div><span className="eyebrow">For makers</span><h1>Your craft, in the spotlight.</h1><p>List your work, set your terms, and meet collectors around the world.</p></div><span className="artisan-chip">✦ Artisan workspace</span></div><div className="artisan-layout"><section className="panel"><div className="panel-heading"><div><h2><Plus size={18} /> Create a listing</h2><p>Tell collectors what makes this piece special.</p></div></div><form onSubmit={submit} className="listing-form"><label>Title<input name="title" value={form.title} onChange={change} placeholder="e.g. Hand-painted Dragon Mask" required /></label><label>Photo URL <span className="optional">or upload below</span><input name="photoUrl" value={form.photoUrl} onChange={change} placeholder="https://…" disabled={Boolean(upload)} /></label><label className={`upload-box ${dragging ? 'dragging' : ''}`} onDragOver={(event) => { event.preventDefault(); setDragging(true) }} onDragLeave={() => setDragging(false)} onDrop={drop}>{upload ? <img src={upload.dataUrl} alt="" style={{ maxHeight: 96, borderRadius: 8 }} /> : <UploadCloud size={22} />}<span>{upload ? <>{upload.name} · <b onClick={(event) => { event.preventDefault(); setUpload(null) }}>remove</b></> : <>Drop an image here or <b>browse files</b></>}</span><small>JPG or PNG · max 5MB</small><input type="file" accept="image/*" hidden onChange={(event) => pick(event.target.files[0])} /></label><div className="form-row"><label>Category<select name="category" value={form.category} onChange={change}>{categories.map((category) => <option key={category}>{category}</option>)}</select></label><label>Starting price<input name="startPrice" type="number" min="1" step="1" value={form.startPrice} onChange={change} placeholder="Nu. 2,500" required /></label><label>Duration<select name="duration" value={form.duration} onChange={change}><option value="2">2 minutes (demo)</option><option value="5">5 minutes</option><option value="30">30 minutes</option><option value="60">1 hour</option></select></label></div>{error && <p className="form-error">{error}</p>}<button className="button button-primary" disabled={saving}>{saving ? 'Creating…' : 'Create draft listing'} <ImagePlus size={16} /></button></form></section><section><div className="section-heading compact"><div><span className="eyebrow">Your inventory</span><h2>My listings</h2></div><span className="muted">{items.length} items</span></div>{loading ? <div className="state-card">Loading your listings…</div> : items.length ? <div className="my-listings">{items.map((item) => <div className="my-listing" key={item.id}><img src={item.photoUrl} alt="" /><div><span className={`status-pill ${item.status.toLowerCase()}`}>{item.status}</span><h3>{item.title}</h3><small>{item.category} · Starting {money(item.startPrice)} {item.status === 'Open' && `· ${item.bids} bids`}</small></div>{item.status === 'Draft' && <button className="button button-small" onClick={() => start(item.id)}><Play size={14} /> Start auction</button>}</div>)}</div> : <div className="state-card"><p>No listings yet.</p></div>}</section></div></div> }
