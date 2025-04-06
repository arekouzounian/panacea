/*

Bootstrap Node for Panacea overlay network.

*/

package main

import (
	"context"
	"fmt"
	mrand "math/rand"

	"github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/multiformats/go-multiaddr"
)

var logger = log.Logger("bootstrap")

func main() {
	log.SetAllLoggers(log.LevelInfo)

	host := "0.0.0.0"
	port := 9001

	fmt.Printf("[*] Listening on: %s with port: %d\n", host, port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := mrand.New(mrand.NewSource(int64(port)))

	// in the future, have a static keypair that we read from
	prvKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	if err != nil {
		panic(err)
	}

	sourceMultiAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", host, port))

	opts := []libp2p.Option{
		libp2p.ListenAddrs(sourceMultiAddr),
		libp2p.Identity(prvKey),
		libp2p.NoSecurity,
	}

	node, err := libp2p.New(opts...)
	if err != nil {
		panic(err)
	}

	logger.Info("Host created. We are:", node.ID())
	logger.Info(node.Addrs())

	_, err = dht.New(ctx, node, dht.Mode(dht.ModeServer), dht.ProtocolPrefix("/panacea"))
	if err != nil {
		panic(err)
	}

	fmt.Println("")
	fmt.Printf("[*] Your Bootstrap ID Is: /ip4/%s/tcp/%d/p2p/%s\n", host, port, node.ID())
	fmt.Println("")
	select {}
}
