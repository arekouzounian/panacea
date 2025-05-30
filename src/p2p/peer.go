package p2p

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/template"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/routing"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"

	"github.com/multiformats/go-multiaddr"
	"google.golang.org/protobuf/proto"

	"github.com/arekouzounian/panacea/chain"
	"github.com/arekouzounian/panacea/ledger"
)

const (
	TopicName               = "panacea-test"
	ProtoPrefix protocol.ID = "panacea"
)

var (
	log = logging.Logger("panacea")
)

type WebInfoHolder struct {
	Identity        string
	AuthorizedPeers []string
	FileHashes      []string

	PeerToAuth map[string][]string
	PeerToRec  map[string][]string
	LocalFiles []string
}

func StartPeer(webServerPort string) {
	// err := logging.SetLogLevel("discovery-util", "debug")
	// if err != nil {
	// 	panic(err)
	// }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// hardcoded bootstrap node
	bootstrapPeer, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/9001/p2p/12D3KooWRbVMva4sB51whN4JvBgKnSLL9jBZHniDnanM8aa1ED1x")
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
		fmt.Println("Searching for peers...")

		disc.Advertise(ctx, TopicName)
		fmt.Println("Advertised ourselves to", TopicName)
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

	// set the new chain to be used internally
	chain.SetLocalChain(bc)

	// set handlers
	// h.SetStreamHandler(chain.PROTOCOL_REQ_CHAIN_EXTENSION, chain.HandleChainHistoryStream)
	h.SetStreamHandler(chain.PROTOCOL_REQ_RECORD_EXTENSION, chain.HandleRecordRequestStream)

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

	go read_from_sub(req_chan, ctx, sub, &h, bc, &sk)

	info := WebInfoHolder{
		Identity:        h.ID().String(),
		FileHashes:      []string{},
		AuthorizedPeers: []string{},
		PeerToAuth:      make(map[string][]string),
		PeerToRec:       make(map[string][]string),
	}

	// Spin up a webserver
	mainHandler := func(w http.ResponseWriter, r *http.Request) {
		t, err := template.ParseFiles("p2p/index.html")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		for _, peer := range state.GetAllAuthPeers() {
			info.PeerToAuth[peer] = state.GetAuthorizedForPeer(peer)
		}
		for _, peer := range state.GetAllRecordPeers() {
			info.PeerToRec[peer] = state.GetRecordsForPeer(peer)
		}

		// get files
		store, err := chain.GetOrMakePeerRecordStore(h.ID().String())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		entries, err := os.ReadDir(store)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		info.LocalFiles = []string{}
		for _, entry := range entries {
			info.LocalFiles = append(info.LocalFiles, entry.Name())
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
			defer file.Close()
			// calculate file digest
			hasher := sha256.New()
			if _, err := io.Copy(hasher, file); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			hash := fmt.Sprintf("%x", hasher.Sum(nil))

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

			store, err := chain.GetOrMakePeerRecordStore(h.ID().String())
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			f, err := os.Create(filepath.Join(store, hash))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			defer f.Close()

			// go back to the start of the file
			file.Seek(0, io.SeekStart)

			if _, err := io.Copy(f, file); err != nil {
				http.Error(w, err.Error(), 500)
				return
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
	syncHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusForbidden)
			return
		}

		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		filename := r.FormValue("name")
		if filename == "" {
			return
		}
		recipient := r.FormValue("recipient")
		if recipient == "" {
			return
		}

		if recipient == h.ID().String() {
			return // spurious
		}

		req := chain.BlockRecord{
			Timestamp:       time.Now().Unix(),
			InitiatorPeerID: h.ID().String(),
			InnerRecord: &chain.BlockRecord_RequestRecord{
				RequestRecord: &chain.Request{
					RequestType: &chain.Request_OriginRecordRequest{
						OriginRecordRequest: &chain.RecordRequestSubmit{
							RecipientPeerID: recipient,
							RecordHash:      filename,
						},
					},
				},
			},
		}

		fmt.Println("sending new block request...")
		new_blk, err := bc.AddLocalBlock(&req, sk)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		req_chan <- new_blk
		bc.PrintChain()
	}

	srv := &http.Server{
		Addr: ":" + webServerPort,
	}
	// defer srv.Shutdown(ctx)
	http.HandleFunc("/", mainHandler)
	http.HandleFunc("/form-submit", formHandler)
	http.HandleFunc("/sync", syncHandler)
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
	// log.Println("Received shutdown signal, attempting graceful shutdown...")
	fmt.Println("Received shutdown signal, attempting graceful shutdown...")
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

func read_from_sub(req_chan chan *chain.Block, ctx context.Context, sub *pubsub.Subscription, host *host.Host, bc *chain.BlockChain, sk *crypto.PrivKey) {
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

			if m.ReceivedFrom == (*host).ID() {
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

			pk := (*host).Peerstore().PubKey(peerID)

			if err = bc.AddForeignBlock(&msg, &pk); err != nil {
				fmt.Printf("Unable to add foreign block: %s\n", err.Error())
				continue
			}

			if block, err := chain.HandlePossibleRequest(ctx, host, &msg); err != nil {
				fmt.Printf("Error handling inner request: %s\n", err.Error())
				continue
			} else if block != nil {
				new_blk, err := bc.AddLocalBlock(block, *sk)
				if err != nil {
					fmt.Printf("unable to create request ack record: %s\n", err.Error())
					continue
				}

				req_chan <- new_blk
			}

			fmt.Println("Block successfully added. Chain state:")
			bc.PrintChain()
		}

	}
}
