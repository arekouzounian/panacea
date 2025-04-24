package ledger

import "testing"

func TestInMemoryEntities(t *testing.T) {
	handler := NewInMemoryStateHandler()
	handler.AddAuthorizedEntities("ASDF", []PeerIDType{"1", "2", "3", "4"})
	handler.RemoveAuthorizedEntities("ASDF", []PeerIDType{"2"})
	handler.RemoveAuthorizedEntities("ASDF", []PeerIDType{"100"})

	if handler.EntityIsAuthorized("ASDF", "2") {
		t.FailNow()
	}
	has := handler.EntityIsAuthorized("ASDF", "1") && handler.EntityIsAuthorized("ASDF", "3") && handler.EntityIsAuthorized("ASDF", "4")

	if !has {
		t.FailNow()
	}
}
