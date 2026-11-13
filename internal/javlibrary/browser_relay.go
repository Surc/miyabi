package javlibrary

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"
)

var ErrBrowserDisconnected = failure("browser", "client_not_connected")

type browserPeerKey struct{}

// WithBrowserPeer identifies the browser's computer by its direct connection.
func WithBrowserPeer(ctx context.Context, remoteAddr string) context.Context {
	return context.WithValue(ctx, browserPeerKey{}, browserPeerIP(remoteAddr))
}

func BrowserPeer(ctx context.Context) string {
	peer, _ := ctx.Value(browserPeerKey{}).(string)
	return peer
}

func browserPeerIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return ""
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	return ip.Unmap().String()
}

type BrowserRelay struct {
	*Client
	token string
	mu    sync.Mutex
	peers map[string]*browserPeer
}

func NewBrowserRelay(config BrowserConfig) *BrowserRelay {
	relay := &BrowserRelay{token: config.APIToken, peers: make(map[string]*browserPeer)}
	relay.Client = newBrowserClient(config, relay.fetch, nil)
	return relay
}

func (r *BrowserRelay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if subtle.ConstantTimeCompare([]byte(req.Header.Get("Authorization")), []byte("Bearer "+r.token)) != 1 {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	ip := browserPeerIP(req.RemoteAddr)
	if req.Header.Get("Origin") != "" || ip == "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if req.ProtoMajor != 1 || !strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		w.WriteHeader(http.StatusUpgradeRequired)
		return
	}
	server := websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(conn *websocket.Conn) {
			r.servePeer(ip, conn)
		},
	}
	server.ServeHTTP(w, req)
}

func (r *BrowserRelay) servePeer(ip string, conn *websocket.Conn) {
	peer := &browserPeer{conn: conn, slot: make(chan struct{}, 1), done: make(chan struct{})}
	conn.MaxPayloadBytes = 6*maxBody + 4096
	r.mu.Lock()
	if r.peers == nil {
		r.mu.Unlock()
		peer.close()
		return
	}
	previous := r.peers[ip]
	r.peers[ip] = peer
	r.mu.Unlock()
	if previous != nil {
		previous.close()
	}
	defer func() {
		close(peer.done)
		r.mu.Lock()
		if r.peers[ip] == peer {
			delete(r.peers, ip)
		}
		r.mu.Unlock()
		peer.close()
	}()
	peer.read()
}

func (r *BrowserRelay) Close() {
	r.mu.Lock()
	peers := r.peers
	r.peers = nil
	r.mu.Unlock()
	for _, peer := range peers {
		peer.close()
	}
}

func (r *BrowserRelay) fetch(ctx context.Context, target string, timeout time.Duration) (browserPage, error) {
	r.mu.Lock()
	peer := r.peers[BrowserPeer(ctx)]
	r.mu.Unlock()
	if peer == nil {
		return browserPage{}, ErrBrowserDisconnected
	}
	return peer.fetch(ctx, target, timeout)
}

type relayRequest struct {
	ID     uint64 `json:"id,omitempty"`
	Cancel uint64 `json:"cancel,omitempty"`
	browserPageRequest
}

type relayReply struct {
	ID uint64 `json:"id"`
	browserPage
}

type browserCall struct {
	id    uint64
	reply chan browserPage
}

type browserPeer struct {
	conn    *websocket.Conn
	slot    chan struct{}
	done    chan struct{}
	writeMu sync.Mutex
	nextID  uint64
	pending atomic.Pointer[browserCall]
}

func (p *browserPeer) close() {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now())
	_ = p.conn.Close()
}

func (p *browserPeer) read() {
	for {
		if p.conn.SetReadDeadline(time.Now().Add(45*time.Second)) != nil {
			return
		}
		var reply relayReply
		if websocket.JSON.Receive(p.conn, &reply) != nil {
			return
		}
		if reply.ID == 0 {
			if p.write(relayRequest{}) != nil {
				return
			}
		} else if call := p.pending.Load(); call != nil && call.id == reply.ID {
			select {
			case call.reply <- reply.browserPage:
			default:
			}
		}
	}
}

func (p *browserPeer) write(message relayRequest) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	select {
	case <-p.done:
		return ErrBrowserDisconnected
	default:
	}
	_ = p.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := websocket.JSON.Send(p.conn, message); err != nil {
		_ = p.conn.Close()
		return ErrBrowserDisconnected
	}
	return nil
}

func (p *browserPeer) fetch(ctx context.Context, target string, timeout time.Duration) (browserPage, error) {
	select {
	case <-ctx.Done():
		return browserPage{}, ctx.Err()
	case <-p.done:
		return browserPage{}, ErrBrowserDisconnected
	case p.slot <- struct{}{}:
		defer func() { <-p.slot }()
	}
	if err := ctx.Err(); err != nil {
		return browserPage{}, err
	}
	p.nextID++
	call := &browserCall{id: p.nextID, reply: make(chan browserPage, 1)}
	p.pending.Store(call)
	defer p.pending.Store(nil)
	if err := p.write(relayRequest{ID: call.id, browserPageRequest: newBrowserPageRequest(ctx, target, timeout)}); err != nil {
		return browserPage{}, err
	}
	select {
	case <-ctx.Done():
		_ = p.write(relayRequest{Cancel: call.id})
		return browserPage{}, ctx.Err()
	case <-p.done:
		return browserPage{}, ErrBrowserDisconnected
	case page := <-call.reply:
		switch page.Error {
		case "":
			return page, nil
		case "verification_required":
			return browserPage{}, ErrVerificationRequired
		default:
			return browserPage{}, failure("browser", "client_page_failed")
		}
	}
}
