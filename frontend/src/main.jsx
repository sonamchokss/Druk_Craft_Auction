import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { Layout } from './components/Layout'
import { AppProvider } from './state/AppContext'
import { Home } from './pages/Home'
import { AuctionRoom } from './pages/AuctionRoom'
import { Artisan } from './pages/Artisan'
import { Buyer } from './pages/Buyer'
import { Admin } from './pages/Admin'
import './styles.css'
function App() { return <AppProvider><BrowserRouter><Layout><Routes><Route path="/" element={<Home />} /><Route path="/auction/:id" element={<AuctionRoom />} /><Route path="/artisan" element={<Artisan />} /><Route path="/buyer" element={<Buyer />} /><Route path="/admin" element={<Admin />} /><Route path="*" element={<Home />} /></Routes></Layout></BrowserRouter></AppProvider> }
ReactDOM.createRoot(document.getElementById('root')).render(<React.StrictMode><App /></React.StrictMode>)
