package p2p

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"text/template"
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

type WebInfoHolder struct {
	Identity        string
	AuthorizedPeers []string
	FileHashes      []string
}

func StartPeer(webServerPort string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// hardcoded bootstrap node
	bootstrapPeer, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/9001/p2p/QmQaUXRLTMDfNm29wTdrvYZ5q1HGHF6LAQrsZ153PJpDE6")
	if err != nil {
		panic(err)
	}
	bootstrapPeerList := []multiaddr.Multiaddr{bootstrapPeer}

	sk, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		panic(err)
	}

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
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		panic(err)
	}

	defer h.Close()

	// connect to the bootstrap node(s)
	for _, addr := range bootstrapPeerList {
		pi, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			panic(err)
		}
		h.Connect(ctx, *pi)
	}

	// bootstrap node should give us a list of adjacent nodes,
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

	// Initialize blockchain and internal state
	state := ledger.NewInMemoryStateHandler()
	bc, err := chain.NewLinkedListBC(state)
	if err != nil {
		panic(err)
	}

	// pubsub; create router and spin up handler threads
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
	defer topic.Close()

	req_chan := make(chan *chain.Block, 1)
	defer close(req_chan)

	go write_to_pub(req_chan, ctx, topic)

	sub, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}
	defer sub.Cancel()

	go read_from_sub(ctx, sub, h, bc)

	info := WebInfoHolder{
		Identity:        h.ID().String(),
		FileHashes:      []string{},
		AuthorizedPeers: []string{},
	}

	// Spin up a webserver
	mainHandler := func(w http.ResponseWriter, r *http.Request) {
		t, err := template.ParseFiles("p2p/index.html")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		t.Execute(w, info)
	}
	formHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusForbidden)
			return
		}

		// max 10mb mem
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		newRecord := chain.BlockRecord{
			Timestamp:       time.Now().Unix(),
			InitiatorPeerID: info.Identity,
		}

		name := r.FormValue("name")
		if name != "" {
			for _, peer := range info.AuthorizedPeers {
				if peer == name {
					http.Error(w, "Peer already added!", 400)
					return // skip duplicates
				}
			}

			info.AuthorizedPeers = append(info.AuthorizedPeers, name)
			newRecord.InnerRecord = &chain.BlockRecord_UpdatePeers{
				UpdatePeers: &chain.AuthorizedPeerUpdate{
					AddedPeerIDs: []string{name},
				},
			}
		}

		file, header, err := r.FormFile("file")
		if err != nil && err != http.ErrMissingFile {
			http.Error(w, err.Error(), 500)
			return
		}

		// edge case: both form fields maliciously submitted
		// in this case we only use the name value
		if name == "" && header != nil {
			// calculate file digest
			h := sha256.New()
			if _, err := io.Copy(h, file); err != nil {
				http.Error(w, err.Error(), 500)
			}
			hash := fmt.Sprintf("%x", h.Sum(nil))

			for _, existing_hash := range info.FileHashes {
				if existing_hash == hash {
					http.Error(w, "Hash already added!", 400)
					return
				}
			}

			info.FileHashes = append(info.FileHashes, hash)
			newRecord.InnerRecord = &chain.BlockRecord_UpdateRecords{
				UpdateRecords: &chain.PeerRecordUpdate{
					AddedRecordHashes: []string{hash},
				},
			}
		}

		blk, err := bc.AddLocalBlock(&newRecord, sk)
		if err != nil {
			fmt.Printf("Error adding local block: %v", err)
			return
		}
		bc.PrintChain() // for debugging purposes

		req_chan <- blk
	}

	srv := &http.Server{
		Addr: ":" + webServerPort,
	}
	// defer srv.Shutdown(ctx)
	http.HandleFunc("/", mainHandler)
	http.HandleFunc("/form-submit", formHandler)
	go func() {
		fmt.Printf("now serving on port %s...\n", webServerPort)
		fmt.Printf("%v", srv.ListenAndServe())
	}()
	defer srv.Shutdown(ctx)

	// exit channel
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	// reached on exit condition
	<-ch
	log.Println("Received shutdown signal, attempting graceful shutdown...")

}

func write_to_pub(req_chan <-chan *chain.Block, ctx context.Context, pub *pubsub.Topic) {
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

func read_from_sub(ctx context.Context, sub *pubsub.Subscription, host host.Host, bc *chain.BlockChain) {
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

			fmt.Println("Received new block. Attempting to add to the chain.")

			var msg chain.Block
			err = proto.Unmarshal(m.Data, &msg)
			if err != nil {
				fmt.Printf("Receiving error: %s\n", err.Error())
				continue
			}

			peerID, err := peer.IDFromBytes(m.From)
			if err != nil {
				fmt.Printf("Error unmarshalling peer: %s\n", err.Error())
				continue
			}

			pk := host.Peerstore().PubKey(peerID)

			if err = bc.AddForeignBlock(&msg, &pk); err != nil {
				fmt.Printf("Unable to add foreign block: %s\n", err.Error())
				continue
			}

			// switch v := msg.RequestType.(type) {
			// case *chain.Request_Example_Request:
			// 	fmt.Printf("Example|%s> %s\n", msg.PeerID[len(msg.PeerID)-16:], v.Example_Request.GetContent())
			// default:
			// 	fmt.Printf("Received unimplemented request type: %v", v)
			// }

			fmt.Println("Block successfully added. Chain state:")
			bc.PrintChain()
		}

	}
}
