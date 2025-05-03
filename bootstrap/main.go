package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

const ProtoPrefix = "panacea"

// PeerTracker tracks the connected peers and their connection status
type PeerTracker struct {
	peers      map[peer.ID]*PeerInfo
	mu         sync.RWMutex
	lastReport time.Time
}

// PeerInfo stores information about connected peers
type PeerInfo struct {
	ID             peer.ID
	Addrs          []multiaddr.Multiaddr
	ConnectedSince time.Time
	LastSeen       time.Time
	ConnectionType string
}

// NewPeerTracker creates a new peer tracker
func NewPeerTracker() *PeerTracker {
	return &PeerTracker{
		peers:      make(map[peer.ID]*PeerInfo),
		lastReport: time.Now(),
	}
}

// AddPeer adds or updates a peer in the tracker
func (pt *PeerTracker) AddPeer(p peer.ID, addrs []multiaddr.Multiaddr, connType string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	now := time.Now()

	if info, exists := pt.peers[p]; exists {
		info.LastSeen = now
		info.Addrs = addrs
		info.ConnectionType = connType
	} else {
		pt.peers[p] = &PeerInfo{
			ID:             p,
			Addrs:          addrs,
			ConnectedSince: now,
			LastSeen:       now,
			ConnectionType: connType,
		}
		// Log new connections immediately
		log.Printf("New peer connected: %s (%s)\n", p.String(), connType)
	}
}

// RemovePeer removes a peer from the tracker
func (pt *PeerTracker) RemovePeer(p peer.ID) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if info, exists := pt.peers[p]; exists {
		log.Printf("Peer disconnected: %s (was connected for %s)\n",
			p.String(), time.Since(info.ConnectedSince).Round(time.Second))
		delete(pt.peers, p)
	}
}

// ReportPeers prints a summary of all connected peers
func (pt *PeerTracker) ReportPeers() {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	now := time.Now()

	if len(pt.peers) == 0 {
		log.Println("No peers currently connected")
		return
	}

	log.Printf("--- Connected Peers Report (%d peers) ---\n", len(pt.peers))
	for _, info := range pt.peers {
		log.Printf("- %s (%s): connected for %s, last seen %s ago\n",
			info.ID.String(),
			info.ConnectionType,
			now.Sub(info.ConnectedSince).Round(time.Second),
			now.Sub(info.LastSeen).Round(time.Second))
	}
	log.Println("---------------------------------------")
}

// ScheduleReports periodically reports peer statistics
func (pt *PeerTracker) ScheduleReports(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pt.ReportPeers()
		case <-ctx.Done():
			return
		}
	}
}

// GetPeerCount returns the number of connected peers
func (pt *PeerTracker) GetPeerCount() int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return len(pt.peers)
}

// GetPeers returns a copy of the peer map
func (pt *PeerTracker) GetPeers() map[peer.ID]PeerInfo {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	result := make(map[peer.ID]PeerInfo, len(pt.peers))
	for id, info := range pt.peers {
		result[id] = *info
	}

	return result
}

// loadOrCreateIdentity either loads an existing node identity or creates a new one
func loadOrCreateIdentity(path string) (crypto.PrivKey, error) {
	privKeyFile := filepath.Join(path, "bootstrap.key")

	// Check if key file exists
	if _, err := os.Stat(privKeyFile); os.IsNotExist(err) {
		// Create directory if it doesn't exist
		err := os.MkdirAll(path, 0700)
		if err != nil {
			return nil, err
		}

		// Generate new key
		priv, _, err := crypto.GenerateKeyPairWithReader(crypto.Ed25519, -1, rand.Reader)
		if err != nil {
			return nil, err
		}

		// Save key to file
		keyBytes, err := crypto.MarshalPrivateKey(priv)
		if err != nil {
			return nil, err
		}

		err = os.WriteFile(privKeyFile, keyBytes, 0600)
		if err != nil {
			return nil, err
		}

		return priv, nil
	} else if err != nil {
		return nil, err
	}

	// Load existing key
	keyBytes, err := os.ReadFile(privKeyFile)
	if err != nil {
		return nil, err
	}

	return crypto.UnmarshalPrivateKey(keyBytes)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listenPort := 9001
	dataDir := "."
	reportInterval := 5 * time.Second

	// Create a peer tracker
	peerTracker := NewPeerTracker()

	// Start periodic reporting of peer statistics
	go peerTracker.ScheduleReports(ctx, reportInterval)

	// Load or create persistent identity
	priv, err := loadOrCreateIdentity(dataDir)
	if err != nil {
		log.Fatal(err)
	}

	// Define addresses to listen on
	listenAddrs := []multiaddr.Multiaddr{}
	addr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", listenPort))
	listenAddrs = append(listenAddrs, addr)
	addr, _ = multiaddr.NewMultiaddr(fmt.Sprintf("/ip6/::/tcp/%d", listenPort))
	listenAddrs = append(listenAddrs, addr)

	// Create libp2p host
	h, err := libp2p.New(
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.Identity(priv),
		libp2p.NATPortMap(),
		libp2p.EnableRelay(),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	// Setup connection notifications
	h.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(n network.Network, conn network.Conn) {
			remotePeer := conn.RemotePeer()
			remoteAddr := conn.RemoteMultiaddr()
			peerTracker.AddPeer(remotePeer, []multiaddr.Multiaddr{remoteAddr}, "direct connection")
		},
		DisconnectedF: func(n network.Network, conn network.Conn) {
			peerTracker.RemovePeer(conn.RemotePeer())
		},
	})

	// Subscribe to peer identification events
	sub, err := h.EventBus().Subscribe(new(event.EvtPeerIdentificationCompleted))
	if err != nil {
		log.Printf("Failed to subscribe to peer identification events: %s\n", err)
	} else {
		defer sub.Close()
		go func() {
			for e := range sub.Out() {
				if evt, ok := e.(event.EvtPeerIdentificationCompleted); ok {
					p := evt.Peer
					addrs := h.Peerstore().Addrs(p)
					peerTracker.AddPeer(p, addrs, "identified peer")
				}
			}
		}()
	}

	// Setup DHT for peer discovery
	kadDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeServer), dht.ProtocolPrefix(ProtoPrefix))
	if err != nil {
		log.Fatal(err)
	}

	// Bootstrap the DHT
	err = kadDHT.Bootstrap(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// start a new pubsub service
	// Doesn't seem to be necessary

	// discovery := drouting.NewRoutingDiscovery(kadDHT)
	// psOpts := []pubsub.Option{
	// 	pubsub.WithDiscovery(discovery),
	// 	pubsub.WithFloodPublish(true),
	// 	pubsub.WithPeerExchange(true),
	// }

	// _, err = pubsub.NewGossipSub(ctx, h, psOpts...)
	// if err != nil {
	// 	panic(err)
	// }

	// Print node info
	log.Println("Bootstrap node started!")
	log.Printf("Node ID: %s\n", h.ID().String())
	log.Println("Listening addresses:")
	for _, addr := range h.Addrs() {
		log.Printf("  %s/p2p/%s\n", addr.String(), h.ID().String())
	}
	log.Println("\nShare the above addresses with peers to connect to this bootstrap node.")
	log.Printf("Peer reports will be printed every %s\n", reportInterval.String())

	// Wait for a SIGINT or SIGTERM signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Println("Received signal, shutting down...")

	// Final peer report
	log.Println("Final peer report before shutdown:")
	peerTracker.ReportPeers()
}
