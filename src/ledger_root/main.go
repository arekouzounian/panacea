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
	CHAIN_ROOT_VAL  = "panacea"
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

func test_signing(input []byte) bool {
	sk_bytes, err := os.ReadFile(KEYFILE_NAME)
	if err != nil {
		panic(err)
	}

	pk_bytes, err := os.ReadFile(PUBKEYFILE_NAME)
	if err != nil {
		panic(err)
	}

	sk, err := crypto.UnmarshalPrivateKey(sk_bytes)
	if err != nil {
		panic(err)
	}
	pk, err := crypto.UnmarshalPublicKey(pk_bytes)
	if err != nil {
		panic(err)
	}

	sig, err := sk.Sign(input)
	if err != nil {
		panic(err)
	}

	verify, err := pk.Verify(input, sig)
	if err != nil {
		panic(err)
	}

	return verify
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

	root_hash := sha256.Sum256([]byte(CHAIN_ROOT_VAL))

	root := &chain.BlockRecord{
		Timestamp:         0,
		InitiatorPeerID:   id.String(),
		PreviousBlockHash: root_hash[:],
		InnerRecord:       nil,
	}

	marshal, err := proto.Marshal(root)
	if err != nil {
		fmt.Printf("Error marshaling block record: %s\n", err.Error())
		return
	}

	hash := sha256.Sum256(marshal)
	signature, err := key.Sign(marshal)
	if err != nil {
		fmt.Printf("Unable to sign hash: %s\n", err.Error())
		return
	}

	if !test_signing(marshal) {
		panic(fmt.Errorf("signature invalid; verification failing"))
	}

	block := &chain.Block{
		Record:    root,
		Hash:      hash[:],
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
