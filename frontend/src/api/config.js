export const config = {
  useMock: import.meta.env.VITE_USE_MOCK !== 'false',
  listing: import.meta.env.VITE_LISTING_API || 'http://localhost:8001',
  bidding: import.meta.env.VITE_BIDDING_API || 'http://localhost:8002',
  payment: import.meta.env.VITE_PAYMENT_API || 'http://localhost:8004',
  notification: import.meta.env.VITE_NOTIFICATION_API || 'http://localhost:8005',
  notificationWs: import.meta.env.VITE_NOTIFICATION_WS || 'ws://localhost:8005',
  rabbit: import.meta.env.VITE_RABBIT_MGMT || 'http://localhost:15672',
}
