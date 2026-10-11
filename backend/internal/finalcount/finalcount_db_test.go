package finalcount_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/totalretail/stocktake/internal/counting"
	"github.com/totalretail/stocktake/internal/export"
	"github.com/totalretail/stocktake/internal/finalcount"
	"github.com/totalretail/stocktake/internal/session"
	"github.com/totalretail/stocktake/internal/testdb"
	"github.com/totalretail/stocktake/internal/variance"
	"github.com/xuri/excelize/v2"
)

// 11 Oct 2026 — the quantity posted to the ERP, the one on the variance screen and the
// one in the Excel export are the same figure. They used to differ: submit took the
// latest line per item, bin and counter; the screen and the export added up every line
// in the item's highest round, so a slot with two rows (a second device, a reinstalled
// app) was counted twice on paper but once in the ERP.
func TestPostedShownAndExportedQuantitiesAgree(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	sid := uuid.NewString()
	bin1, bin2 := uuid.NewString(), uuid.NewString()
	ada, ben := uuid.NewString(), uuid.NewString()
	t0 := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)

	for _, it := range []session.SessionItem{
		{SessionID: sid, ItemNo: "A", Description: "Sugar", UnitCost: 2},
		{SessionID: sid, ItemNo: "B", Description: "Rice", UnitCost: 3},
		{SessionID: sid, ItemNo: "C", Description: "Salt", UnitCost: 1},
	} {
		if err := db.Create(&it).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Create(&session.TheoreticalStock{SessionID: sid, ItemNo: "A", TheoreticalQty: 12})
	line := func(item, bin, counter string, qty float64, round int, at time.Time) {
		t.Helper()
		l := counting.CountLine{SessionID: sid, ItemNo: item, BinID: bin, CounterID: counter, Quantity: qty,
			RoundNo: round, CountedAt: at, ClientUUID: uuid.NewString()}
		if err := db.Create(&l).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A: Ada counted bin 1 twice (5, then 7 from a second device): 7 counts. Ben counted bin 2: 3. A = 10.
	line("A", bin1, ada, 5, 1, t0)
	line("A", bin1, ada, 7, 1, t0.Add(time.Minute))
	line("A", bin2, ben, 3, 1, t0)
	// B: round 1 said 4, the recount (round 2) said 6. B = 6.
	line("B", bin1, ada, 4, 1, t0)
	line("B", bin1, ada, 6, 2, t0.Add(time.Hour))
	// C: never counted. C = 0.
	want := map[string]float64{"A": 10, "B": 6, "C": 0}

	posted, err := finalcount.Totals(ctx, db, sid)
	if err != nil {
		t.Fatal(err)
	}
	for item, q := range want {
		if posted[item] != q {
			t.Errorf("posted %s = %v, want %v", item, posted[item], q)
		}
	}

	shown, err := variance.NewService(db).GetConsolidated(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(shown) != 3 {
		t.Fatalf("consolidated has %d lines, want 3", len(shown))
	}
	for _, l := range shown {
		if l.CountedQty != want[l.ItemNo] {
			t.Errorf("variance screen %s = %v, want %v", l.ItemNo, l.CountedQty, want[l.ItemNo])
		}
	}
	if a := shown[0]; a.ItemNo != "A" || a.Variance != -2 || a.VarianceCost != -4 {
		t.Errorf("A's variance: got %+v, want counted 10 vs 12, variance -2, cost -4", a)
	}

	path, err := export.GenerateSessionExport(ctx, db, sid, "Test store", "2026-10-11", "FLOOR", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := f.GetRows("Stock Count", excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rows[4:] {
		if len(r) >= 4 && r[0] != "" {
			got[r[0]] = r[3]
		}
	}
	for item, q := range map[string]string{"A": "10", "B": "6", "C": "0"} {
		if got[item] != q {
			t.Errorf("export %s = %q, want %q", item, got[item], q)
		}
	}
}
