package live

import (
	"errors"
	"sync"
	"sync/atomic"
)

var (
	// ErrRoomFull means the room is at its participant cap.
	ErrRoomFull = errors.New("live: room full")
	// ErrTooManyConns means one IP is holding too many connections.
	ErrTooManyConns = errors.New("live: too many connections")
)

// outboundQueue bounds how far a peer can fall behind before we drop it. Once the
// queue fills, the peer is a slow consumer and gets disconnected.
const outboundQueue = 64

// Peer is one connected participant. The transport layer drains Send in its write
// pump and watches Dead to learn it was dropped (slow consumer or room teardown).
type Peer struct {
	ID   uint64
	slug string
	ip   string
	send chan []byte
	dead chan struct{}
	once sync.Once
}

// Send is the outbound queue the write pump drains.
func (p *Peer) Send() <-chan []byte { return p.send }

// Dead closes when the peer must disconnect.
func (p *Peer) Dead() <-chan struct{} { return p.dead }

// Deliver queues a message straight to this peer (e.g. an error or peers count),
// dropping the peer if it can't keep up.
func (p *Peer) Deliver(msg []byte) { p.enqueue(msg) }

func (p *Peer) kill() { p.once.Do(func() { close(p.dead) }) }

// enqueue hands a message to the peer, dropping (killing) it if it can't keep up.
func (p *Peer) enqueue(msg []byte) {
	select {
	case p.send <- msg:
	default:
		p.kill()
	}
}

type room struct {
	peers map[*Peer]struct{}
}

// Hub tracks connected peers per room and fans out messages between them. All
// state is in memory; the store owns durability. It never spawns goroutines.
type Hub struct {
	mu             sync.Mutex
	rooms          map[string]*room
	ipConns        map[string]int
	nextID         atomic.Uint64
	maxPerRoom     int
	maxPerIP       int
	closed         bool
}

func NewHub(maxPerRoom, maxPerIP int) *Hub {
	return &Hub{
		rooms:      make(map[string]*room),
		ipConns:    make(map[string]int),
		maxPerRoom: maxPerRoom,
		maxPerIP:   maxPerIP,
	}
}

// Join registers a new peer, enforcing the room and per-IP caps.
func (h *Hub) Join(slug, ip string) (*Peer, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrRoomFull
	}
	if h.maxPerIP > 0 && h.ipConns[ip] >= h.maxPerIP {
		return nil, ErrTooManyConns
	}
	r := h.rooms[slug]
	if r == nil {
		r = &room{peers: make(map[*Peer]struct{})}
		h.rooms[slug] = r
	}
	if len(r.peers) >= h.maxPerRoom {
		return nil, ErrRoomFull
	}
	p := &Peer{
		ID:   h.nextID.Add(1),
		slug: slug,
		ip:   ip,
		send: make(chan []byte, outboundQueue),
		dead: make(chan struct{}),
	}
	r.peers[p] = struct{}{}
	h.ipConns[ip]++
	return p, nil
}

// Leave removes a peer and tidies up empty rooms.
func (h *Hub) Leave(p *Peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[p.slug]
	if r == nil {
		return
	}
	if _, ok := r.peers[p]; !ok {
		return
	}
	delete(r.peers, p)
	if len(r.peers) == 0 {
		delete(h.rooms, p.slug)
	}
	if h.ipConns[p.ip] > 0 {
		h.ipConns[p.ip]--
		if h.ipConns[p.ip] == 0 {
			delete(h.ipConns, p.ip)
		}
	}
}

// Broadcast fans msg out to every peer in the room except the sender.
func (h *Hub) Broadcast(slug string, sender *Peer, msg []byte) {
	h.mu.Lock()
	r := h.rooms[slug]
	if r == nil {
		h.mu.Unlock()
		return
	}
	targets := make([]*Peer, 0, len(r.peers))
	for p := range r.peers {
		if p != sender {
			targets = append(targets, p)
		}
	}
	h.mu.Unlock()

	// Enqueue outside the lock; a slow peer just gets killed by enqueue.
	for _, p := range targets {
		p.enqueue(msg)
	}
}

// Count reports how many peers are currently in a room.
func (h *Hub) Count(slug string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[slug]; r != nil {
		return len(r.peers)
	}
	return 0
}

// Close disconnects every peer and refuses new joins; used at server shutdown.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, r := range h.rooms {
		for p := range r.peers {
			p.kill()
		}
	}
}
