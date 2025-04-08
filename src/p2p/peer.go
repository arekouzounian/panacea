package p2p

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/routing"

	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/multiformats/go-multiaddr"
)

const (
	TopicName               = "panacea-test"
	ProtoPrefix protocol.ID = "panacea"
)

func StartPeer() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// hardcoded bootstrap node
	bootstrapPeer, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/9001/p2p/QmQaUXRLTMDfNm29wTdrvYZ5q1HGHF6LAQrsZ153PJpDE6")
	if err != nil {
		panic(err)
	}
	var bootstrapPeerList = []multiaddr.Multiaddr{bootstrapPeer}

	var idht *dht.IpfsDHT

	opts := []libp2p.Option{
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {

			// Initialize the DHT for peer discovery/routing.
			// Also begin the bootstrap process.
			var err error
			idht, err = dht.New(ctx, h, dht.ProtocolPrefix(ProtoPrefix))
			if err != nil {
				return idht, err
			}

			err = idht.Bootstrap(ctx)
			return idht, err
		}),
		libp2p.NoSecurity, // TODO: support TLS/Noise
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		panic(err)
	}

	// connect to the bootstrap node(s)
	for _, addr := range bootstrapPeerList {
		pi, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			panic(err)
		}
		h.Connect(ctx, *pi)
	}

	// bootstrap noe should give us a list of adjacent nodes,
	// connect to those as well.
	// Set a short timeout and then re-advertise ourselves if needed
	disc := drouting.NewRoutingDiscovery(idht)

	anyConnected := false
	for !anyConnected {
		dutil.Advertise(ctx, disc, TopicName)
		fmt.Println("Advertised ourselves to", TopicName)
		fmt.Println("Searching for peers...")
		peerChan, err := disc.FindPeers(ctx, TopicName)
		if err != nil {
			panic(err)
		}
		for peer := range peerChan {
			if peer.ID == h.ID() {
				continue // No self connection
			}
			err := h.Connect(ctx, peer)
			if err != nil {
				fmt.Printf("Failed connecting to %s, error: %s\n", peer.ID, err)
			} else {
				fmt.Println("Connected to:", peer.ID)
				anyConnected = true
			}
		}

		if !anyConnected {
			time.Sleep(5 * time.Second)
		}
	}

	// pubsub
	psOpts := []pubsub.Option{
		pubsub.WithFloodPublish(true),
		pubsub.WithPeerExchange(true),
		pubsub.WithMessageIdFn(pubsub.DefaultMsgIdFn),
	}

	ps, err := pubsub.NewGossipSub(ctx, h, psOpts...)
	if err != nil {
		panic(err)
	}
	topic, err := ps.Join(TopicName)
	if err != nil {
		panic(err)
	}

	go func(ctx context.Context, topic *pubsub.Topic) {
		reader := bufio.NewReader(os.Stdin)
		for {
			fmt.Printf("> ")
			s, err := reader.ReadString('\n')
			if err != nil {
				panic(err)
			}
			if err := topic.Publish(ctx, []byte(s)); err != nil {
				fmt.Println("### Publish error:", err)
			}
		}
	}(ctx, topic)

	sub, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}

	go func(ctx context.Context, sub *pubsub.Subscription) {
		for {
			m, err := sub.Next(ctx)
			if err != nil {
				panic(err)
			}

			if m.ReceivedFrom == h.ID() {
				continue
			}

			fmt.Printf("%s> %s", m.ReceivedFrom, string(m.Message.Data))
		}
	}(ctx, sub)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Println("Received signal, shutting down...")

}
