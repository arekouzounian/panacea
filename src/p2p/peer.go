package p2p

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/routing"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/multiformats/go-multiaddr"
	"google.golang.org/protobuf/proto"

	"github.com/arekouzounian/panacea/chain"
	"github.com/arekouzounian/panacea/ledger"
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
	bootstrapPeerList := []multiaddr.Multiaddr{bootstrapPeer}

	sk, _, err := crypto.GenerateEd25519Key(rand.Reader)

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
		libp2p.Identity(sk),
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

	marshalPeerID, err := h.ID().MarshalBinary()
	if err != nil {
		panic(err)
	}

	// pubsub
	psOpts := []pubsub.Option{
		pubsub.WithFloodPublish(true),
		pubsub.WithPeerExchange(true),
		pubsub.WithMessageIdFn(pubsub.DefaultMsgIdFn),
	}

	state := ledger.NewInMemoryStateHandler()
	bc, err := chain.NewLinkedListBC(state)

	ps, err := pubsub.NewGossipSub(ctx, h, psOpts...)
	if err != nil {
		panic(err)
	}
	topic, err := ps.Join(TopicName)
	if err != nil {
		panic(err)
	}
	defer topic.Close()

	req_chan := make(chan *chain.Request, 1)
	defer close(req_chan)

	go write_to_pub(req_chan, ctx, topic)

	sub, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}
	defer sub.Cancel()

	go read_from_sub(ctx, sub, h)

	// Enter REPL loop to get user input

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	scanner := bufio.NewScanner(os.Stdin)
outer_loop:
	for {
		select {
		case <-ch:
			log.Println("Received signal, shutting down...")
			break outer_loop
		default:
			fmt.Print("Enter a message: ")
			scanner.Scan()
			msg := scanner.Text()

			if len(msg) < 1 {
				continue
			}

			switch msg {
			// case "1":
			// 	fmt.Println("Sending history request")
			// 	marshal := chain.Request{
			// 		PeerID:    h.ID().String(),
			// 		Timestamp: time.Now().Unix(),
			// 		RequestType: &chain.Request_History_Request{
			// 			History_Request: &chain.Request_ChainHistory{
			// 				AfterHash: chain.CHAIN_ROOT,
			// 			},
			// 		},
			// 	}

			// 	req_chan <- &marshal
			default:
				inner_record := chain.EmptyRecord{
					Msg: msg,
				}

				outer_record := chain.BlockRecord{
					Timestamp:       time.Now().Unix(),
					InitiatorPeerID: marshalPeerID,
					InnerRecord: &chain.BlockRecord_TestRecord{
						TestRecord: &inner_record,
					},
				}

				block, err := SignRecordToBlock(sk, &outer_record)
				if err != nil {
					fmt.Printf("Error signing record: %s\n", err)
					continue
				}

				err = bc.AddBlock(block)
				if err != nil {
					fmt.Printf("Unable to extend blockchain: %s", err)
				}

				fmt.Println()
				bc.PrintChain()
			}
		}
	}
}

func write_to_pub(req_chan <-chan *chain.Request, ctx context.Context, pub *pubsub.Topic) {
	for req := range req_chan {

		encoded, err := proto.Marshal(req)
		if err != nil {
			panic(err)
		}

		if err := pub.Publish(ctx, encoded); err != nil {
			fmt.Println("### Publish error:", err)
		}
	}
}

func read_from_sub(ctx context.Context, sub *pubsub.Subscription, host host.Host) {
outer_sub_loop:
	for {
		select {
		case <-ctx.Done():
			break outer_sub_loop
		default:
			m, err := sub.Next(ctx)
			if err != nil {
				break outer_sub_loop
			}
			if m == nil {
				continue
			}

			if m.ReceivedFrom == host.ID() {
				continue
			}

			var msg chain.Request
			err = proto.Unmarshal(m.Data, &msg)
			if err != nil {
				fmt.Println("Receiving error:", err.Error())
				continue
			}

			switch v := msg.RequestType.(type) {
			case *chain.Request_Example_Request:
				fmt.Printf("Example|%s> %s\n", msg.PeerID[len(msg.PeerID)-16:], v.Example_Request.GetContent())
			default:
				fmt.Printf("Received unimplemented request type: %v", v)
			}
		}

	}
}
