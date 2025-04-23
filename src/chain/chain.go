package chain

type RecordClass interface {
	ListFields() []string
}

type TransactionBlock struct {
	Timestamp         int64
	InitiatorPeerID   string
	Hash              string
	PreviousBlockHash string
	Record            RecordClass
	Signature         []byte
}

type AddAuthorizedEntitiesRecord struct {
	PeerID             string
	AuthorizedEntities []string
}

func (r *AddAuthorizedEntitiesRecord) ListFields() []string {
	return r.AuthorizedEntities
}

type RemoveAuthorizedEntitiesRecord struct {
	PeerID            string
	EntityRemovalList []string
}

func (r *RemoveAuthorizedEntitiesRecord) ListFields() []string {
	return r.EntityRemovalList
}
