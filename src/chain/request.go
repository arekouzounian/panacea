package chain

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"google.golang.org/protobuf/proto"
)

const (
	PROTOCOL_REQ_CHAIN_EXTENSION  = "panacea/request/chain"
	PROTOCOL_REQ_RECORD_EXTENSION = "panacea/request/record"
	PEER_RECORD_STORE_LOC         = "peer-records"
)

// Local peer record store; stores relevant files
// defaults to proj_root/peer-records/<peerID>/
func GetOrMakePeerRecordStore(peerID string) (string, error) {
	root, err := os.Getwd()
	ret := ""
	if err != nil {
		return ret, fmt.Errorf("unable to locate executable: %s", err.Error())
	}
	filepath.Dir(root)

	ret = filepath.Join(root, PEER_RECORD_STORE_LOC, peerID)

	if err = os.MkdirAll(ret, 0777); err != nil {
		return ret, fmt.Errorf("unable to create peerstore %s: %s", ret, err.Error())
	}

	return ret, nil
}

// Checks if there are any requests in the block that need to be serviced.
// Should be called after a new block is added.
//
// If the block pointer is non-nil, it is a new block to be broadcast/added.
//
// Admittedly this is a bit messy. The whole coupling with the blockchain pointer
// could probably be reworked for clarity, and the functionality could be integrated into the
// add block call.
func HandlePossibleRequest(ctx context.Context, h *host.Host, record *Block) (*BlockRecord, error) {
	// if it isn't a request, return.
	request, is_record := record.Record.InnerRecord.(*BlockRecord_RequestRecord)
	if !is_record {
		return nil, nil
	}

	// This is bad practice.
	// We should instead probably be marshalling the peer ID objs directly into
	// our protobuf so that we don't have to keep doing this.
	var peerID *peer.ID
	for _, peer := range (*h).Peerstore().Peers() {
		if peer.String() == record.Record.InitiatorPeerID {
			peerID = &peer
			break
		}
	}
	if peerID == nil {
		return nil, fmt.Errorf("peer found that's not in peerstore: %s", record.Record.InitiatorPeerID)
	}

	// start a different sequence based on the request.
	switch innerRequest := request.RequestRecord.RequestType.(type) {
	case *Request_ChainHistoryRequest:
		// Send the node each block we have.
		fmt.Println("received a chain extension request")
		stream, err := (*h).NewStream(ctx, *peerID, PROTOCOL_REQ_CHAIN_EXTENSION)
		if err != nil {
			return nil, fmt.Errorf("unable to start new stream with peer: %s", err.Error())
		}

		req := innerRequest.ChainHistoryRequest
		for _, block := range bc.BlocksAfterHash(req.AfterHash) {
			marshal, err := proto.Marshal(block)
			if err != nil {
				return nil, fmt.Errorf("error marshalling block in chain history request response: %s", err.Error())
			}

			length := uint32(len(marshal))
			err = binary.Write(stream, binary.BigEndian, length)
			if err != nil {
				return nil, fmt.Errorf("error sending length encoding over the wire")
			}

			if _, err := stream.Write(marshal); err != nil {
				return nil, fmt.Errorf("error sending block over the wire")
			}
		}

	case *Request_OriginRecordRequest:
		// if we match the request, validate them and send them the record

		stream, err := (*h).NewStream(ctx, *peerID, PROTOCOL_REQ_RECORD_EXTENSION)
		if err != nil {
			return nil, fmt.Errorf("unable to start new stream with peer: %s", err.Error())
		}

		req := innerRequest.OriginRecordRequest
		if req.RecipientPeerID != (*h).ID().String() {
			break
		}

		// now validate that they're part of our ACL
		ack := false

		// attempt to send them the file over the stream.
		// if we encounter any errors here, we don't end up sending a new block record
		// and we just return the error.
		if bc.IsAuthorizedEntity((*h).ID().String(), record.Record.InitiatorPeerID) {
			ack = true

			fmt.Println("authorized entity making request for record, attempting to send")
			fmt.Printf("Sending record %s to entity %s\n", req.RecordHash, record.Record.InitiatorPeerID)

			store, err := GetOrMakePeerRecordStore((*h).ID().String())
			if err != nil {
				return nil, err
			}

			fpath := filepath.Join(store, req.RecordHash)

			b, err := os.ReadFile(fpath)
			if err != nil {
				return nil, fmt.Errorf("unable to read file %s to service request: %s", fpath, err.Error())
			}

			err = binary.Write(stream, binary.BigEndian, uint32(len(b)))
			if err != nil {
				return nil, fmt.Errorf("unable to send length encoding over the wire: %s", err.Error())
			}

			// In the future might want to buffer these writes
			stream.Write(b)
		}

		new_record := BlockRecord{
			Timestamp:       time.Now().Unix(),
			InitiatorPeerID: (*h).ID().String(),
			InnerRecord: &BlockRecord_RequestRecord{
				RequestRecord: &Request{
					RequestType: &Request_RecordRequestResponse{
						RecordRequestResponse: &RecordRequestAck{
							RequesterPeerID: record.Record.InitiatorPeerID,
							RecordHash:      req.RecordHash,
							RequestSuccess:  ack,
						},
					},
				},
			},
		}

		return &new_record, nil

	}

	return nil, nil
}

func HandleRecordRequestStream(s network.Stream) {
	fmt.Println("received new record request stream")
	defer s.Close()

	var msg_len uint32
	err := binary.Read(s, binary.BigEndian, &msg_len)
	if err != nil {
		fmt.Printf("error receiving length encoding from stream: %s\n", err.Error())
		return
	}

	buf := make([]byte, msg_len)
	_, err = s.Read(buf)
	if err != nil {
		fmt.Printf("error receiving record from stream: %s\n", err.Error())
		return
	}

	store, err := GetOrMakePeerRecordStore(s.Conn().LocalPeer().String())

	if err != nil {
		fmt.Printf("error trying to get local record store: %s\n", err.Error())
		return
	}

	digest := sha256.Sum256(buf)
	new_path := filepath.Join(store, fmt.Sprintf("%x", digest))

	err = os.WriteFile(new_path, buf, 0660)
	if err != nil {
		fmt.Printf("error writing file to local store: %s\n", err.Error())
		return
	}

	fmt.Printf("successfully stored queried record at %s.\n", new_path)
}

func HandleChainHistoryStream(s network.Stream) {
	fmt.Println("received new chain sync stream")
	defer s.Close()

	// read all the blocks in sequence

	new_blocks := []*Block{}

	bc := GetLocalChain()
	if bc == nil {
		return
	}

	for {
		var msg_len uint32
		binary.Read(s, binary.BigEndian, &msg_len)

		if msg_len == 0 {
			break
		}

		buf := make([]byte, msg_len)
		var new_block Block
		_, err := s.Read(buf)
		if err != nil {
			fmt.Printf("error reading block encoding from stream: %s\n", err.Error())

		}

		err = proto.Unmarshal(buf, &new_block)
		if err != nil {
			fmt.Printf("Couldn't unmarshal block encoding: %s\n", err.Error())
			return
		}

		new_blocks = append(new_blocks, &new_block)
	}

	bc.SyncLocalState(new_blocks)

	s.Close()
}
