import { Header } from './Header'
import { Toast } from './Toast'
import { useApp } from '../state/AppContext'
export function Layout({ children }) { const { toast } = useApp(); return <><Header /><main>{children}</main><Toast toast={toast} /><footer><span>✦ Druk Craft Auction</span><span>Celebrating the hands of Bhutan</span></footer></> }
