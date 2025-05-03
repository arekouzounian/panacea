package chain

import (
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"slices"
	sync "sync"
	"time"

	"github.com/arekouzounian/panacea/ledger"
	"github.com/libp2p/go-libp2p/core/crypto"
	"google.golang.org/protobuf/proto"
)

const (
	GENESIS_BLOCK_LOC  = "ledger_root/genesis.block"
	MASTER_PUB_KEY_LOC = "ledger_root/master.pub"
	CHAIN_ROOT_VAL     = "panacea"
)

var (
	bc *BlockChain
)

func GetLocalChain() *BlockChain {
	return bc
}

func SetLocalChain(b *BlockChain) {
	bc = b
}

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

func (b *BlockChain) BlocksAfterHash(hash []byte) []*Block {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	var startNode *BlockNode
	if hash == nil {
		startNode = b.head
	} else {
		curr := b.head
		for curr != nil && !slices.Equal(curr.block.Hash, hash) {
			curr = curr.next
		}

		if curr == nil {
			return []*Block{}
		}
		startNode = curr
	}

	ret := []*Block{}
	for startNode != nil {
		ret = append(ret, startNode.block)
		startNode = startNode.next
	}

	return ret
}

func (b *BlockChain) IsAuthorizedEntity(entityID string, candidateID string) bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	return b.innerState.EntityIsAuthorized(ledger.PeerIDType(entityID), ledger.PeerIDType(candidateID))
}

// Everything should be filled in other than previous block hash
func (b *BlockChain) AddLocalBlock(record *BlockRecord, key crypto.PrivKey) (*Block, error) {
	newBlock, err := b.CreateBlockWithoutAdding(record, key)
	if err != nil {
		return nil, err
	}

	b.mutex.Lock()
	defer b.mutex.Unlock()

	err = b.addBlock(newBlock)
	if err != nil {
		return nil, err
	}

	return newBlock, nil
}

func (b *BlockChain) CreateBlockWithoutAdding(record *BlockRecord, key crypto.PrivKey) (*Block, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	record.PreviousBlockHash = b.head.block.Hash

	marshal, err := proto.Marshal(record)
	if err != nil {
		return nil, err
	}

	sig, err := key.Sign(marshal)
	if err != nil {
		return nil, err
	}

	digest := sha256.Sum256(marshal)
	newBlock := Block{
		Record:    record,
		Hash:      digest[:],
		Signature: sig,
	}

	return &newBlock, nil
}

func (b *BlockChain) AddForeignBlock(record *Block, key *crypto.PubKey) error {
	if req, is_req := record.Record.InnerRecord.(*BlockRecord_RequestRecord); is_req {
		if _, is_chain_history := req.RequestRecord.RequestType.(*Request_ChainHistoryRequest); is_chain_history {
			return nil // no need to add
		}
	}

	if err := b.isValidProposedBlock(record, key); err != nil {
		return fmt.Errorf("invalid block proposal: %s", err.Error())
	}

	b.mutex.Lock()
	defer b.mutex.Unlock()

	return b.addBlock(record)
}

func (b *BlockChain) isValidProposedBlock(record *Block, fromPubKey *crypto.PubKey) error {
	// TODO
	b.mutex.Lock()
	defer b.mutex.Unlock()

	marshal, err := proto.Marshal(record.Record)
	if err != nil {
		return err
	}

	if valid_sig, err := (*fromPubKey).Verify(marshal, record.Signature); !valid_sig || err != nil {
		return err
	}

	// Prev block hash should match
	if !slices.Equal(b.head.block.Hash, record.Record.PreviousBlockHash) {
		return fmt.Errorf("previous block hash doesn't match")
	}

	// hash value should match
	digest := sha256.Sum256(marshal)

	if !slices.Equal(digest[:], record.Hash) {
		return fmt.Errorf("inner record hash value doesn't match contents")
	}

	return nil
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
	// Unhandled case: Request record
	// This is handled by subsequently calling HandlePossibleRequest(...) in `request.go`
	switch v := record.Record.InnerRecord.(type) {
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
	}

	return nil
}

// can also use a receiver arg
// to get the head hash
func CreateChainHistoryRequest(peerID string) *BlockRecord {
	return &BlockRecord{
		Timestamp:       time.Now().Unix(),
		InitiatorPeerID: peerID,
		InnerRecord: &BlockRecord_RequestRecord{
			RequestRecord: &Request{
				RequestType: &Request_ChainHistoryRequest{
					ChainHistoryRequest: &ChainHistory{
						AfterHash: []byte{},
					},
				},
			},
		},
	}
}

func (b *BlockChain) SyncLocalState(new_blocks []*Block) {
	// we've just received a new list of blocks
	// step through them, make sure that each one is valid
	// then add new blocks that come after ours
	if len(new_blocks) < 1 {
		fmt.Println("given no blocks!")
		return
	}

	b.mutex.Lock()
	defer b.mutex.Unlock()

	fmt.Println("Chain sync response triggered, updating chain...")

	curr := b.root
	curr_block_iter := 0

	for curr != nil && !slices.Equal(curr.block.Hash, new_blocks[curr_block_iter].Hash) {
		curr = curr.next
	}
	if curr == nil {
		fmt.Println("invalid sequence.")
		return // invalid sequence; there must be some overlap
	}

	// now we've found a part in our chain that matches a part in the proposed chain
	// curr points to the first matching block in our chain
	// we want to iterate new_blocks until we find a block that doesn't match our chain
	var prv *BlockNode
	for curr != nil && curr_block_iter < len(new_blocks) && slices.Equal(curr.block.Hash, new_blocks[curr_block_iter].Hash) {
		prv = curr
		curr = curr.next
		curr_block_iter += 1
	}
	if curr_block_iter >= len(new_blocks) || curr != nil {
		fmt.Println("overlap doesn't match")
		return
	}

	blocks_to_add := new_blocks[curr_block_iter:]

	prv_hash := prv.block.Record.PreviousBlockHash

	for _, proposed_block := range blocks_to_add {
		if !slices.Equal(proposed_block.Hash, prv_hash) {
			fmt.Println("presented an inconsistent chain")
			return
		}
		// also want to validate signatures here
	}

	// at this point, we have new valid blocks
	// add each one!
	// we already hold the mutex
	for _, proposed_block := range blocks_to_add {
		b.addBlock(proposed_block)
	}

	// chain sync complete
	fmt.Println("chain sync complete. chain state:")
	b.PrintChain()
}

// for debug purposes
func (b *BlockChain) PrintChain() {
	i := 0
	for curr := b.root; curr != nil; curr = curr.next {
		rec := curr.block.Record

		var recType string
		if curr == b.root {
			recType = "Genesis Record"
		} else {
			switch inner := rec.InnerRecord.(type) {
			case *BlockRecord_UpdatePeers:
				recType = "Update Authorized Peers"
			case *BlockRecord_UpdateRecords:
				recType = "Update Records"
			case *BlockRecord_RequestRecord:
				recType = "Request Record "
				switch inner.RequestRecord.RequestType.(type) {
				case *Request_ChainHistoryRequest:
					recType += "(Chain History Request)"
				case *Request_OriginRecordRequest:
					recType += "(Origin Record Request)"
				case *Request_RecordRequestResponse:
					recType += "(Record Request Response)"
				}
			}
		}

		fmt.Printf("Block %d:\n\tInitiator: %s\n\tRecord Type: %s\n", i, rec.InitiatorPeerID, recType)
		i += 1
	}
}
