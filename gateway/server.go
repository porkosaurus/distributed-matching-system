package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// upgrader converts a regular HTTP connection to a WebSocket connection
// CheckOrigin: always return true means we accept connections from any origin
// fine for development — you'd restrict this in production
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// hub tracks all connected WebSocket clients and broadcasts fills to them
type hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
}

func newHub() *hub {
	return &hub{
		clients: make(map[*websocket.Conn]bool),
	}
}

func (h *hub) add(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = true
	h.mu.Unlock()
}

func (h *hub) remove(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
	conn.Close()
}

// broadcast sends a fill to every connected client as JSON
func (h *hub) broadcast(fill Fill) {
	data, err := json.Marshal(fill)
	if err != nil {
		log.Printf("error marshaling fill: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for conn := range h.clients {
		err := conn.WriteMessage(websocket.TextMessage, data)
		if err != nil {
			log.Printf("error broadcasting to client: %v", err)
		}
	}
}

// runBroadcaster reads fills from fillCh and broadcasts to all clients
func runBroadcaster(h *hub, fillCh <-chan Fill) {
	for fill := range fillCh {
		h.broadcast(fill)
	}
}

// handleConnection is called once per WebSocket client connection
// it reads orders from that client and sends them to the sequencer
func handleConnection(conn *websocket.Conn, h *hub, orderCh chan<- Order) {
	defer h.remove(conn)

	log.Printf("client connected: %s", conn.RemoteAddr())

	for {
		// read the next message — blocks until a message arrives
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("client disconnected: %s", conn.RemoteAddr())
			return
		}

		// parse JSON into an Order struct
		var order Order
		if err := json.Unmarshal(msg, &order); err != nil {
			log.Printf("bad order from client: %v", err)
			continue // skip bad messages, keep connection open
		}

		// basic validation
		if order.Side != "buy" && order.Side != "sell" {
			log.Printf("invalid side: %s", order.Side)
			continue
		}
		if order.Price == 0 || order.Quantity == 0 {
			log.Printf("invalid order: zero price or quantity")
			continue
		}

		// send to sequencer — non-blocking send with a helpful error
		// if the channel is full it means the engine is overwhelmed
		select {
		case orderCh <- order:
			// sent successfully
		default:
			log.Printf("order channel full — dropping order %d", order.ID)
		}
	}
}

// startServer starts the WebSocket HTTP server
func startServer(addr string, h *hub, orderCh chan<- Order) {
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("upgrade error: %v", err)
			return
		}
		h.add(conn)
		// each client gets its own goroutine — they run concurrently
		go handleConnection(conn, h, orderCh)
	})

	log.Printf("WebSocket server listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}