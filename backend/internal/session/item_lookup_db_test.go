package session_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/totalretail/stocktake/internal/session"
	"github.com/totalretail/stocktake/internal/testdb"
	"github.com/totalretail/stocktake/internal/ws"
)

// 11 Oct 2026 — the recount list opens an item by its item number; a phone that has not
// cached the session's items asks the server, which used to look only at barcodes and
// answer "not found". A variant's item number ("1001/RED") works the same way.
func TestAnItemIsFoundByBarcodeOrItemNumber(t *testing.T) {
	db := testdb.New(t)
	svc := session.NewService(db, &fakeERP{}, ws.NewHub())
	sid := uuid.NewString()
	db.Create(&session.SessionItem{SessionID: sid, ItemNo: "1001/RED", Barcode: "6001234500017", Description: "Shirt red"})

	for _, code := range []string{"6001234500017", "1001/RED"} {
		it, err := svc.GetSessionItemByBarcode(context.Background(), sid, code)
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		if it.ItemNo != "1001/RED" {
			t.Errorf("%s found %s", code, it.ItemNo)
		}
	}
	if _, err := svc.GetSessionItemByBarcode(context.Background(), sid, "nothing"); err == nil {
		t.Error("an unknown code was found")
	}
}
