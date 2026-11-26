package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/totalretail/stocktake/internal/store"
	"github.com/totalretail/stocktake/internal/testdb"
)

// 25 Nov 2026 — editing a store must not deactivate it, and adding a store whose code
// belongs to an inactive store brings that store back instead of failing.
func TestStoreEditKeepsItActiveAndReAddReactivates(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	svc := store.NewService(db)

	st, err := svc.CreateStore(ctx, store.Store{StoreCode: "5001", StoreName: "Super Market 01", LSStoreCode: "5001"})
	if err != nil || !st.Active {
		t.Fatalf("create: %+v %v", st, err)
	}
	// The edit page sends only these fields (no "active", no "created_at").
	up, err := svc.UpdateStore(ctx, store.Store{ID: st.ID, StoreCode: "5001", StoreName: "Kadoma Main", LSStoreCode: "5001", LocationCode: "5001"})
	if err != nil || !up.Active || up.StoreName != "Kadoma Main" || up.CreatedAt.IsZero() {
		t.Fatalf("update: %+v %v", up, err)
	}
	if list, _ := svc.ListStores(ctx); len(list) != 1 {
		t.Fatalf("after edit the store is listed: %+v", list)
	}
	if _, err := svc.CreateStore(ctx, store.Store{StoreCode: "5001", StoreName: "Again"}); !errors.Is(err, store.ErrStoreCodeInUse) {
		t.Fatalf("duplicate of an active store: %v", err)
	}

	db.Exec(`UPDATE stores SET active = false WHERE id = ?`, st.ID)
	back, err := svc.CreateStore(ctx, store.Store{StoreCode: "5001", StoreName: "Kadoma Main 2", LSStoreCode: "5001"})
	if err != nil || back.ID != st.ID || !back.Active || back.StoreName != "Kadoma Main 2" {
		t.Fatalf("re-adding an inactive store: %+v %v", back, err)
	}

	other, _ := svc.CreateStore(ctx, store.Store{StoreCode: "5002", StoreName: "Kadoma Express", LSStoreCode: "5002"})
	if _, err := svc.UpdateStore(ctx, store.Store{ID: other.ID, StoreCode: "5001", StoreName: "x"}); !errors.Is(err, store.ErrStoreCodeInUse) {
		t.Fatalf("editing to another store's code: %v", err)
	}
}
