package chain

import (
	"crypto/sha256"
	"slices"

	"google.golang.org/protobuf/proto"
)

const (
	// The root value of the tree.
	CHAIN_ROOT = "panacea"

	// This is just a precomputed SHA-256 hash of the above.
	ROOT_SHA256_STR = "90f1adb96d94e0fcc5a619e01bced753809a841cb9c6c5707d3d6cb447b40eba"
)

// No constant arrays in go...?
var (
	ROOT_SHA256 = []byte{0x90, 0xf1, 0xad, 0xb9, 0x6d, 0x94, 0xe0, 0xfc, 0xc5, 0xa6, 0x19, 0xe0, 0x1b, 0xce, 0xd7, 0x53, 0x80, 0x9a, 0x84, 0x1c, 0xb9, 0xc6, 0xc5, 0x70, 0x7d, 0x3d, 0x6c, 0xb4, 0x47, 0xb4, 0x0e, 0xba}
)

type MerkleNode struct {
	Hash       []byte
	LeftChild  *MerkleNode
	RightChild *MerkleNode
}

type MerkleTree struct {
	root *MerkleNode

	leaf_nodes []*MerkleNode
}

func NewMerkleTree(genesis *Block) (MerkleTree, error) {
	t := MerkleTree{}

	bytes, err := proto.Marshal(genesis)
	if err != nil {
		return t, err
	}

	hash := sha256.Sum256(bytes)

	t.root = &MerkleNode{
		Hash:       hash[:],
		LeftChild:  nil,
		RightChild: nil,
	}

	t.leaf_nodes = []*MerkleNode{t.root}

	return t, nil
}

func (t *MerkleTree) HasLeaf(hash []byte) bool {
	for _, leaf := range t.leaf_nodes {
		if slices.Equal(leaf.Hash, hash) {
			return true
		}
	}
	return false
}

func (t *MerkleTree) LastBlockHash() []byte {
	if t.root == nil {
		return nil
	}

	var dfs func(node *MerkleNode) []byte
	dfs = func(node *MerkleNode) []byte {
		if node.RightChild != nil {
			return dfs(node.RightChild)
		} else if node.LeftChild != nil {
			return dfs(node.LeftChild)
		} else {
			return node.Hash
		}
	}

	return dfs(t.root)
}

func (t *MerkleTree) AddBlock(digest []byte) error {
	new_node := &MerkleNode{
		Hash:       digest,
		LeftChild:  nil,
		RightChild: nil,
	}

	t.leaf_nodes = append(t.leaf_nodes, new_node)

	// calculate new tree

	return nil
}
