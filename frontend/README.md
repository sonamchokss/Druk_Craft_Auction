# Frontend — owner: Sonam Wangmo

React (Vite) or plain HTML/JS. Serve on port **3000**, then uncomment the `frontend` block in `docker-compose.yml`.

Service base URLs (CORS is already enabled on every service):

| Service | URL |
|---|---|
| Listing | http://localhost:8001 |
| Bidding | http://localhost:8002 |
| Timer | http://localhost:8003 |
| Payment | http://localhost:8004 |
| Notification (REST + WebSocket) | http://localhost:8005 |

Errors come back as `{"error": "Bid too low"}`. Show that text to the user.
