package p2p

import (
	"crypto/sha256"

	"github.com/arekouzounian/panacea/chain"
	"github.com/libp2p/go-libp2p/core/crypto"
	"google.golang.org/protobuf/proto"
)

func SignRecordToBlock(sk crypto.PrivKey, rec *chain.BlockRecord) (*chain.Block, error) {
	b, err := proto.Marshal(rec)
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256(b)
	sig, err := sk.Sign(hash[:])
	if err != nil {
		return nil, err
	}

	return &chain.Block{
		Record:    rec,
		Hash:      hash[:],
		Signature: sig,
	}, nil
}
