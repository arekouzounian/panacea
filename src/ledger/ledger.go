package ledger

type PeerIDType string
type RecordHashType string
type EntityRecordSet map[RecordHashType]bool
type EntityAuthSet map[PeerIDType]bool

/*
Chain will hold an audit log of transactions.
Each transaction is an update to a client state:
- add authorized entities for client
- remove authorized entities for client
- add record hashes under entity's control
- remove record hashes under entity's control
- entity B requests record from entity A
- entity A denies request from entity B
- entity A accepts request from entity B
*/

type StateHandler interface {
	AddAuthorizedEntities(EntityID PeerIDType, AuthorizedEntities []PeerIDType)
	RemoveAuthorizedEntities(EntityID PeerIDType, AuthorizedEntities []PeerIDType)

	AddRecords(EntityID PeerIDType, RecordHashes []RecordHashType)
	RemoveRecords(EntityID PeerIDType, RecordHashes []RecordHashType)

	EntityIsAuthorized(EntityID PeerIDType, QueryEntity PeerIDType) bool
	EntityHasRecord(EntityID PeerIDType, RecordHash RecordHashType) bool

	GetAuthorizedForPeer(PeerID string) []string
	GetRecordsForPeer(PeerID string) []string

	GetAllAuthPeers() []string
	GetAllRecordPeers() []string
}

// This isn't going to be very optimized.
type InMemoryStateHandler struct {
	entityRecords       map[PeerIDType]EntityRecordSet
	authorizedEntityMap map[PeerIDType]EntityAuthSet
}

func NewInMemoryStateHandler() *InMemoryStateHandler {
	return &InMemoryStateHandler{
		entityRecords:       make(map[PeerIDType]EntityRecordSet),
		authorizedEntityMap: make(map[PeerIDType]EntityAuthSet),
	}
}

// helper func: convert an []string into an []PeerIDType
//
// This seems inconvenient but in case we expand the peer id type
// in the future it'll be nice to have this
func ToPeerIDTypeSlice(conv *[]string) []PeerIDType {
	s := make([]PeerIDType, len(*conv))

	for i, id := range *conv {
		s[i] = PeerIDType(id)
	}

	return s
}

func ToRecordHashTypeSlice(conv *[]string) []RecordHashType {
	s := make([]RecordHashType, len(*conv))

	for i, hash := range *conv {
		s[i] = RecordHashType(hash)
	}

	return s
}

func (h *InMemoryStateHandler) AddAuthorizedEntities(EntityID PeerIDType, AuthorizedEntities []PeerIDType) {
	if _, exists := h.authorizedEntityMap[EntityID]; !exists {
		h.authorizedEntityMap[EntityID] = make(EntityAuthSet)
	}

	for _, entity := range AuthorizedEntities {
		h.authorizedEntityMap[EntityID][entity] = true
	}

}

func (h *InMemoryStateHandler) RemoveAuthorizedEntities(EntityID PeerIDType, AuthorizedEntities []PeerIDType) {
	if _, exists := h.authorizedEntityMap[EntityID]; !exists {
		h.authorizedEntityMap[EntityID] = make(EntityAuthSet)
	}

	// Could just set them to false as well
	for _, entity := range AuthorizedEntities {
		delete(h.authorizedEntityMap[EntityID], entity)
	}
}

func (h *InMemoryStateHandler) AddRecords(EntityID PeerIDType, RecordHashes []RecordHashType) {
	if _, exists := h.entityRecords[EntityID]; !exists {
		h.entityRecords[EntityID] = make(EntityRecordSet)
	}

	for _, record := range RecordHashes {
		h.entityRecords[EntityID][record] = true
	}
}

func (h *InMemoryStateHandler) RemoveRecords(EntityID PeerIDType, RecordHashes []RecordHashType) {
	if _, exists := h.entityRecords[EntityID]; !exists {
		h.entityRecords[EntityID] = make(EntityRecordSet)
	}

	for _, record := range RecordHashes {
		delete(h.entityRecords[EntityID], record)
	}
}

func (h *InMemoryStateHandler) EntityIsAuthorized(EntityID PeerIDType, QueryEntity PeerIDType) bool {
	if _, exists := h.authorizedEntityMap[EntityID]; !exists {
		h.authorizedEntityMap[EntityID] = make(EntityAuthSet)
		return false
	}

	_, is_authorized := h.authorizedEntityMap[EntityID][QueryEntity]

	return is_authorized
}

func (h *InMemoryStateHandler) EntityHasRecord(EntityID PeerIDType, RecordHash RecordHashType) bool {
	if _, exists := h.entityRecords[EntityID]; !exists {
		h.entityRecords[EntityID] = make(EntityRecordSet)
		return false
	}

	_, has_record := h.entityRecords[EntityID][RecordHash]

	return has_record
}

func (h *InMemoryStateHandler) GetAuthorizedForPeer(peerID string) []string {
	cast := PeerIDType(peerID)

	ret := []string{}
	if _, exists := h.authorizedEntityMap[cast]; !exists {
		return ret
	}

	set := h.authorizedEntityMap[cast]

	for peer, is_auth := range set {
		if is_auth {
			ret = append(ret, string(peer))
		}
	}

	return ret
}

func (h *InMemoryStateHandler) GetRecordsForPeer(peerID string) []string {
	cast := PeerIDType(peerID)
	ret := []string{}
	if _, exists := h.entityRecords[cast]; !exists {
		return ret
	}

	for rec, exists := range h.entityRecords[cast] {
		if exists {
			ret = append(ret, string(rec))
		}
	}

	return ret
}

func (h *InMemoryStateHandler) GetAllAuthPeers() []string {
	ret := []string{}

	for peer := range h.authorizedEntityMap {
		ret = append(ret, string(peer))
	}

	return ret
}
func (h *InMemoryStateHandler) GetAllRecordPeers() []string {
	ret := []string{}

	for peer := range h.entityRecords {
		ret = append(ret, string(peer))
	}

	return ret
}
