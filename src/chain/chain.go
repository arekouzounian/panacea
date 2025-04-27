package chain

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"os"

	"github.com/arekouzounian/panacea/ledger"
	"google.golang.org/protobuf/proto"
)

const (
	GENESIS_BLOCK_LOC = "ledger_root/genesis.block"
)

var (
	hasher hash.Hash = sha256.New()
)

// type BlockChain interface {
// 	AddBlock(record *Block) error
// 	HasBlock(hash string) error

// 	BlocksAfterHash(hash string) ([]Block, error)
// }

type BlockChain struct {
	root *BlockNode
	head *BlockNode

	mTree      MerkleTree
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
		return nil, err
	}

	var genesis Block

	err = proto.Unmarshal(b, &genesis)
	if err != nil {
		return nil, err
	}

	tree, err := NewMerkleTree(&genesis)
	if err != nil {
		return nil, err
	}

	rootNode := &BlockNode{
		block: &genesis,
		prev:  nil,
		next:  nil,
	}

	return &BlockChain{
		root: rootNode,
		head: rootNode,

		mTree:      tree,
		innerState: stateHandler,
	}, nil

}

// returns an error if the previous block pointer is already set
// takes in a proposed block, extends the chain, sets previous block hashes,
// and re-hashes if necessary
func (b *BlockChain) AddBlock(record *Block) error {
	fmt.Println("attempting to add to the chain!")

	marshal, err := proto.Marshal(record)
	if err != nil {
		return err
	}

	digest := hasher.Sum(marshal)
	if b.mTree.HasLeaf(digest) {
		return fmt.Errorf("attempting to insert duplicate digest: %v already exists in the merkle tree", digest)
	}

	// entity_conv, err := peer.IDFromBytes(record.Record.InitiatorPeerID)
	// if err != nil {
	// 	return err
	// }

	record.Record.PreviousBlockHash = b.mTree.LastBlockHash()

	wrapper := BlockNode{
		block: record,
		prev:  b.head,
		next:  nil,
	}

	if err = b.mTree.AddBlock(digest); err != nil {
		return err
	}

	// only update the state if adding to the tree isn't erroneous
	b.head.next = &wrapper
	b.head = &wrapper

	// update internal state
	switch v := record.Record.InnerRecord.(type) {
	case *BlockRecord_TestRecord:
		fmt.Printf("test call to blockchain state: %s", v.TestRecord.Msg)
	// case *BlockRecord_UpdatePeers:
	// b.innerState.AddAuthorizedEntities(),
	default:
		fmt.Println("I haven't implemented that transaction handler yet!")
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
