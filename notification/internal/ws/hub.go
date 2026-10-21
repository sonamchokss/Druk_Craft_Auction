package ws

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sonamchokss/druk-craft-auction/notification/internal/model"
)

type client struct {
	conn *websocket.Conn
	send chan any
}
type Hub struct {
	mu           sync.RWMutex
	users        map[string]map[*client]struct{}
	eventClients map[*client]struct{}
	events       []*model.Envelope
}

func NewHub() *Hub {
	return &Hub{users: map[string]map[*client]struct{}{}, eventClients: map[*client]struct{}{}}
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func (h *Hub) ServeUser(w http.ResponseWriter, r *http.Request, userID string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, send: make(chan any, 16)}
	h.add(userID, c)
	defer h.remove(userID, c)
	go h.write(c)
	conn.SetReadLimit(1 << 20)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error { conn.SetReadDeadline(time.Now().Add(60 * time.Second)); return nil })
	for {
		if _, _, err = conn.ReadMessage(); err != nil {
			return
		}
	}
}
func (h *Hub) ServeEvents(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, send: make(chan any, 64)}
	h.mu.Lock()
	snapshot := append([]*model.Envelope(nil), h.events...)
	h.eventClients[c] = struct{}{}
	h.mu.Unlock()
	for _, event := range snapshot {
		c.send <- event
	}
	defer func() {
		h.mu.Lock()
		delete(h.eventClients, c)
		h.mu.Unlock()
		close(c.send)
		conn.Close()
	}()
	go h.write(c)
	for {
		if _, _, err = conn.ReadMessage(); err != nil {
			return
		}
	}
}
func (h *Hub) add(user string, c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.users[user] == nil {
		h.users[user] = map[*client]struct{}{}
	}
	h.users[user][c] = struct{}{}
}
func (h *Hub) remove(user string, c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.users[user], c)
	close(c.send)
}
func (h *Hub) write(c *client) {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case value, ok := <-c.send:
			if !ok {
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteJSON(value); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
func (h *Hub) SendUser(user string, n model.Notification) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.users[user] {
		select {
		case c.send <- n:
		default:
		}
	}
}
func (h *Hub) SendEvent(event model.Envelope) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, &event)
	if len(h.events) > 50 {
		h.events = h.events[len(h.events)-50:]
	}
	for c := range h.eventClients {
		select {
		case c.send <- event:
		default:
		}
	}
}
