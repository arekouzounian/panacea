package chain

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"log"
	"os"

	"github.com/arekouzounian/panacea/ledger"
	"github.com/libp2p/go-libp2p/core/crypto"
	"google.golang.org/protobuf/proto"
)

const (
	GENESIS_BLOCK_LOC  = "ledger_root/genesis.block"
	MASTER_PUB_KEY_LOC = "ledger_root/master.pub"
)

const (
	// The root value of the tree.
	CHAIN_ROOT = "panacea"

	// This is just a precomputed SHA-256 hash of the above.
	ROOT_SHA256_STR = "90f1adb96d94e0fcc5a619e01bced753809a841cb9c6c5707d3d6cb447b40eba"
)

// No constant arrays in go...?
var (
	ROOT_SHA256           = []byte{0x90, 0xf1, 0xad, 0xb9, 0x6d, 0x94, 0xe0, 0xfc, 0xc5, 0xa6, 0x19, 0xe0, 0x1b, 0xce, 0xd7, 0x53, 0x80, 0x9a, 0x84, 0x1c, 0xb9, 0xc6, 0xc5, 0x70, 0x7d, 0x3d, 0x6c, 0xb4, 0x47, 0xb4, 0x0e, 0xba}
	hasher      hash.Hash = sha256.New()
)

// type BlockChain interface {
// 	AddBlock(record *Block) error
// 	HasBlock(hash string) error

// 	BlocksAfterHash(hash string) ([]Block, error)
// }

type BlockChain struct {
	root *BlockNode
	head *BlockNode

	innerState ledger.StateHandler
}

type BlockNode struct {
	block *Block
	prev  *BlockNode
	next  *BlockNode
}

// Initializes an empty blockchain with only the coin base
func NewLinkedListBC(stateHandler ledger.StateHandler) (*BlockChain, error) {
	b, err := os.ReadFile(GENESIS_BLOCK_LOC)
	if err != nil {
		return nil, fmt.Errorf("unable to find genesis block at ./%s: %v", GENESIS_BLOCK_LOC, err)
	}

	keyBytes, err := os.ReadFile(MASTER_PUB_KEY_LOC)
	if err != nil {
		return nil, fmt.Errorf("unable to read chain public key at ./%s: %v", MASTER_PUB_KEY_LOC, err)
	}

	pubkey, err := crypto.UnmarshalPublicKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal public key: %v", err)
	}

	var genesis Block

	err = proto.Unmarshal(b, &genesis)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal genesis block: %v", err)
	}

	if verified, err := pubkey.Verify(b, genesis.Signature); err != nil {
		return nil, fmt.Errorf("unable to verify signature of block: %v", err)
	} else if !verified {
		return nil, fmt.Errorf("genesis block signature doesn't match: %v", err)
	}

	rootNode := &BlockNode{
		block: &genesis,
		prev:  nil,
		next:  nil,
	}

	return &BlockChain{
		root: rootNode,
		head: rootNode,

		innerState: stateHandler,
	}, nil
}

func (b *BlockChain) IsValidBlockAddition(record *Block) bool {
	// TODO
	return false
}

// Takes a block and simply extends the current linked list.
// Doesn't provide any checking; block hashes must be validated by the user.
func (b *BlockChain) AddBlock(record *Block) error {
	if b.head == nil {
		return fmt.Errorf("attempting to add to a nil blockchain; no genesis block presented")
	}

	wrapper := BlockNode{
		block: record,
		prev:  b.head,
		next:  nil,
	}

	b.head.next = &wrapper
	b.head = &wrapper

	initiator := ledger.PeerIDType(record.Record.InitiatorPeerID)

	// update internal state
	switch v := record.Record.InnerRecord.(type) {
	case *BlockRecord_TestRecord:
		log.Printf("test call to blockchain state: %s", v.TestRecord.Msg)
	case *BlockRecord_UpdatePeers:
		log.Printf("Update Peers block added to the chain from peer %s", record.Record.InitiatorPeerID)
		log.Printf("Added Peers: %v", v.UpdatePeers.AddedPeerIDs)
		log.Printf("Removed Peers: %v", v.UpdatePeers.RemovedPeerIDs)

		// Update internal state
		b.innerState.AddAuthorizedEntities(initiator, ledger.ToPeerIDTypeSlice(&v.UpdatePeers.AddedPeerIDs))
		b.innerState.RemoveAuthorizedEntities(initiator, ledger.ToPeerIDTypeSlice(&v.UpdatePeers.RemovedPeerIDs))

	case *BlockRecord_UpdateRecords:
		log.Printf("Update Records block added to the chain from peer %s", record.Record.InitiatorPeerID)
		log.Printf("Added Records: %v", v.UpdateRecords.AddedRecordHashes)
		log.Printf("Removed Records: %v", v.UpdateRecords.RemovedRecordHashes)

		// update internal state
		b.innerState.AddRecords(initiator, ledger.ToRecordHashTypeSlice(&v.UpdateRecords.AddedRecordHashes))
		b.innerState.RemoveRecords(initiator, ledger.ToRecordHashTypeSlice(&v.UpdateRecords.RemovedRecordHashes))

	default:
		log.Printf("Caught unimplemented record type: %v", v)
	}

	return nil
}

// for debug purposes
func (b *BlockChain) PrintChain() {
	i := 0
	for curr := b.root; curr != nil; curr = curr.next {
		rec := curr.block.Record

		var recType string
		switch rec.InnerRecord.(type) {
		case *BlockRecord_TestRecord:
			recType = "Test record"
		case *BlockRecord_UpdatePeers:
			recType = "Update Authorized Peers"
		case *BlockRecord_UpdateRecords:
			recType = "Update Records"
		default:
			recType = "unknown record"
		}

		fmt.Printf("Block %d:\n\tInitiator: %s\n\tRecord Type: %s\n", i, rec.InitiatorPeerID, recType)
		i += 1
	}
}
