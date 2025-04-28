package chain

import (
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"slices"
	sync "sync"

	"github.com/arekouzounian/panacea/ledger"
	"github.com/libp2p/go-libp2p/core/crypto"
	"google.golang.org/protobuf/proto"
)

const (
	GENESIS_BLOCK_LOC  = "ledger_root/genesis.block"
	MASTER_PUB_KEY_LOC = "ledger_root/master.pub"
	CHAIN_ROOT_VAL     = "panacea"
)

func ValidateMarshalledGenesisBlock(block_bytes []byte) (*Block, error) {
	keyBytes, err := os.ReadFile(MASTER_PUB_KEY_LOC)
	if err != nil {
		return nil, fmt.Errorf("unable to read chain public key at ./%s: %v", MASTER_PUB_KEY_LOC, err)
	}

	pubkey, err := crypto.UnmarshalPublicKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal public key: %v", err)
	}

	var genesis Block

	err = proto.Unmarshal(block_bytes, &genesis)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal genesis block: %v", err)
	}

	inner_bytes, err := proto.Marshal(genesis.Record)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal genesis block inner record: %v", err)
	}

	if verified, err := pubkey.Verify(inner_bytes, genesis.Signature); err != nil {
		return nil, fmt.Errorf("unable to verify signature of block: %v", err)
	} else if !verified {
		return nil, fmt.Errorf("genesis block signature doesn't match: %v", err)
	}

	root_hash := sha256.Sum256([]byte(CHAIN_ROOT_VAL))

	if !slices.Equal(genesis.Record.PreviousBlockHash, root_hash[:]) {
		return nil, fmt.Errorf("genesis block previous hash doesn't match: got (%x), wanted %x", genesis.Record.PreviousBlockHash, root_hash)
	}

	return &genesis, nil
}

type BlockChain struct {
	root *BlockNode
	head *BlockNode

	innerState ledger.StateHandler
	mutex      sync.Mutex
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

	genesis, err := ValidateMarshalledGenesisBlock(b)
	if err != nil {
		return nil, err
	}

	rootNode := &BlockNode{
		block: genesis,
		prev:  nil,
		next:  nil,
	}

	return &BlockChain{
		root: rootNode,
		head: rootNode,

		innerState: stateHandler,
		mutex:      sync.Mutex{},
	}, nil
}

func (b *BlockChain) IsValidBlockAddition(record *Block) bool {
	// TODO
	return false
}

// Everything should be filled in other than previous block hash
func (b *BlockChain) AddLocalBlock(record *BlockRecord, key crypto.PrivKey) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	record.PreviousBlockHash = b.head.block.Hash

	marshal, err := proto.Marshal(record)
	if err != nil {
		return err
	}

	sig, err := key.Sign(marshal)
	if err != nil {
		return err
	}

	digest := sha256.Sum256(marshal)

	newBlock := Block{
		Record:    record,
		Hash:      digest[:],
		Signature: sig,
	}

	return b.addBlock(&newBlock)
}

// Takes a block and simply extends the current linked list.
// Doesn't provide any checking; block hashes must be validated by the user.
//
// Not thread-safe. Mutex must be locked beforehand.
func (b *BlockChain) addBlock(record *Block) error {
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
