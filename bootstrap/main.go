/*

Bootstrap Node for Panacea overlay network.

*/

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/multiformats/go-multiaddr"
)

var logger = log.Logger("bootstrap")

const keyFilePath = "bootstrap.key"

func loadOrGenKey() (crypto.PrivKey, error) {
	// key exists, read and return
	if _, err := os.Stat(keyFilePath); err == nil {
		bytes, err := os.ReadFile(keyFilePath)
		if err != nil {
			return nil, err
		}

		return crypto.UnmarshalPrivateKey(bytes)
	}

	// gen new key
	prvKey, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
	if err != nil {
		return nil, err
	}

	// marshal it for later
	data, err := crypto.MarshalPrivateKey(prvKey)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(keyFilePath, data, 0600); err != nil {
		return nil, err
	}

	return prvKey, nil
}

func main() {
	log.SetAllLoggers(log.LevelInfo)

	host := "0.0.0.0"
	port := 9001

	fmt.Printf("[*] Listening on: %s with port: %d\n", host, port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// in the future, have a static keypair that we read from
	prvKey, err := loadOrGenKey()
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
