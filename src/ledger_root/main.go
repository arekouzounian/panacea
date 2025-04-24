package main

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"github.com/arekouzounian/panacea/chain"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"google.golang.org/protobuf/proto"
)

const (
	KEYFILE_NAME    = "master.key"
	PUBKEYFILE_NAME = "master.pub"
)

func loadOrCreateIdentity(path string) (crypto.PrivKey, error) {
	privKeyFile := filepath.Join(path, KEYFILE_NAME)
	pubKeyFile := filepath.Join(path, PUBKEYFILE_NAME)

	if _, err := os.Stat(privKeyFile); os.IsNotExist(err) {
		// Create directory if it doesn't exist
		err := os.MkdirAll(path, 0700)
		if err != nil {
			return nil, err
		}

		// Generate new key
		priv, pub, err := crypto.GenerateKeyPairWithReader(crypto.Ed25519, -1, rand.Reader)
		if err != nil {
			return nil, err
		}

		// Save key to file
		keyBytes, err := crypto.MarshalPrivateKey(priv)
		if err != nil {
			return nil, err
		}

		pubBytes, err := crypto.MarshalPublicKey(pub)
		if err != nil {
			return nil, err
		}

		err = os.WriteFile(privKeyFile, keyBytes, 0600)
		if err != nil {
			return nil, err
		}

		err = os.WriteFile(pubKeyFile, pubBytes, 0666)
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
	key, err := loadOrCreateIdentity(".")
	if err != nil {
		fmt.Printf("Unable to load or generate keypair: %s\n", err.Error())
		return
	}

	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		fmt.Printf("Unable to formulate identity: %s\n", err.Error())
		return
	}

	marshal_id, err := id.MarshalBinary()
	if err != nil {
		fmt.Printf("Unable to marshal identity: %s\n", err.Error())
		return
	}

	root := &chain.BlockRecord{
		Timestamp:         0,
		InitiatorPeerID:   marshal_id,
		PreviousBlockHash: nil,
		InnerRecord:       nil,
	}

	marshal, err := proto.Marshal(root)
	if err != nil {
		fmt.Printf("Error marshaling block record: %s\n", err.Error())
		return
	}

	hash := sha256.Sum256(marshal)
	signature, err := key.Sign(hash[:])
	if err != nil {
		fmt.Printf("Unable to sign hash: %s\n", err.Error())
		return
	}

	block := &chain.Block{
		Record:    root,
		Signature: signature,
	}

	marshal, err = proto.Marshal(block)
	if err != nil {
		fmt.Printf("Couldn't marshal block: %s\n", err.Error())
		return
	}

	os.WriteFile("genesis.block", marshal, os.FileMode(0666))
	f, err := os.Create("genesis.block")
	if err != nil {
		fmt.Printf("Couldn't create genesis.block: %s\n", err.Error())
		return
	}
	defer f.Close()

	f.Write(marshal)
}
