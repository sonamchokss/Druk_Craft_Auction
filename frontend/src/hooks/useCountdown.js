import { useEffect, useState } from 'react'
export function useCountdown(endsAt) {
  const [remaining, setRemaining] = useState(() => Math.max(0, new Date(endsAt).getTime() - Date.now()))
  useEffect(() => { const tick = () => setRemaining(Math.max(0, new Date(endsAt).getTime() - Date.now())); tick(); const timer = setInterval(tick, 250); return () => clearInterval(timer) }, [endsAt])
  const total = Math.floor(remaining / 1000)
  return { remaining, expired: remaining <= 0, label: `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}` }
}
