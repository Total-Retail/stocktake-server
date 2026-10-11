package variance_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/totalretail/stocktake/internal/counting"
	"github.com/totalretail/stocktake/internal/session"
	"github.com/totalretail/stocktake/internal/settings"
	"github.com/totalretail/stocktake/internal/testdb"
	"github.com/totalretail/stocktake/internal/variance"
)

// Stream D (11 Oct 2026) — the variance report uses the tolerance set for the session's
// stock count type, not VARIANCE_TOLERANCE_PCT, which is only the fallback.
func TestTheVarianceReportUsesTheTypesTolerance(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	settings.SeedDefaultVarianceSettings(db)
	db.Model(&settings.VarianceSetting{}).Where("stock_count_type = ?", "BAKERY").Update("tolerance_pct", 5)

	sid := uuid.NewString()
	ws := "1"
	db.Create(&session.Session{ID: sid, StoreID: uuid.NewString(), StockCountDate: "2026-10-11", Type: "BAKERY",
		Status: session.StatusActive, WorksheetNo: &ws, CreatedBy: uuid.NewString()})
	bin, who := uuid.NewString(), uuid.NewString()
	for item, counted := range map[string]float64{"LOAF": 103, "ROLL": 107} { // 3% and 7% over 100
		db.Create(&session.SessionItem{SessionID: sid, ItemNo: item})
		db.Create(&session.TheoreticalStock{SessionID: sid, ItemNo: item, TheoreticalQty: 100})
		db.Create(&counting.CountLine{SessionID: sid, ItemNo: item, BinID: bin, CounterID: who, Quantity: counted, RoundNo: 1, ClientUUID: uuid.NewString()})
	}

	svc := variance.NewService(db)
	tol := svc.ToleranceFor(ctx, sid, 2)
	if tol != 5 {
		t.Fatalf("tolerance = %v, want BAKERY's 5", tol)
	}
	lines, err := svc.GetVarianceReport(ctx, sid, tol)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].ItemNo != "ROLL" {
		t.Errorf("report = %+v, want ROLL only (7%% > 5%%; LOAF's 3%% is within)", lines)
	}
	if got := svc.ToleranceFor(ctx, uuid.NewString(), 2); got != 2 {
		t.Errorf("unknown session: tolerance %v, want the fallback 2", got)
	}
}
