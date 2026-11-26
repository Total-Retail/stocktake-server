package labels_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/totalretail/stocktake/internal/auth"
	"github.com/totalretail/stocktake/internal/devices"
	"github.com/totalretail/stocktake/internal/labels"
	"github.com/totalretail/stocktake/internal/testdb"
	"github.com/totalretail/stocktake/internal/vantage"
	"github.com/totalretail/stocktake/pkg/middleware"
)

// fakeBC keeps label worksheets in memory, as Business Central would.
type fakeBC struct {
	mu      sync.Mutex
	sheets  map[string]*vantage.LabelWorksheet
	lines   map[string]*vantage.LabelWorksheetLine
	created []vantage.NewLabelWorksheet
	scans   [][]vantage.LabelScan
	patched map[string]string // line id -> If-Match used
	deleted []string
	refuse  error // returned by AddLabelScans when set
}

func newFakeBC() *fakeBC {
	return &fakeBC{sheets: map[string]*vantage.LabelWorksheet{}, lines: map[string]*vantage.LabelWorksheetLine{}, patched: map[string]string{}}
}

func (f *fakeBC) ListLabelWorksheets(_ context.Context, flt vantage.LabelWorksheetFilter) ([]vantage.LabelWorksheet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []vantage.LabelWorksheet
	for _, w := range f.sheets {
		if (flt.OwnerID == "" || w.OwnerID == flt.OwnerID) && (flt.StoreNo == "" || w.StoreNo == flt.StoreNo) &&
			(flt.Status == "" || w.Status == flt.Status) {
			out = append(out, *w)
		}
	}
	return out, nil
}

func (f *fakeBC) GetLabelWorksheet(_ context.Context, id string, withLines bool) (*vantage.LabelWorksheet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.sheets[id]
	if !ok {
		return nil, vantage.ErrLabelNotFound
	}
	cp := *w
	cp.Lines = nil
	if withLines {
		for _, l := range f.lines {
			if l.WorksheetID == id {
				cp.Lines = append(cp.Lines, *l)
			}
		}
	}
	return &cp, nil
}

func (f *fakeBC) CreateLabelWorksheet(_ context.Context, in vantage.NewLabelWorksheet) (*vantage.LabelWorksheet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in)
	w := &vantage.LabelWorksheet{ID: uuid.NewString(), ETag: `W/"1"`, Number: "LW0000" + string(rune('0'+len(f.sheets)+1)),
		StoreNo: in.StoreNo, Description: in.Description, Status: "Open", OwnerID: in.OwnerID, OwnerName: in.OwnerName,
		DeviceID: in.DeviceID, CreatedAt: "2026-11-24T08:00:00Z", LastPrintedAt: "0001-01-01T00:00:00Z"}
	f.sheets[w.ID] = w
	return w, nil
}

func (f *fakeBC) UpdateLabelWorksheetDescription(_ context.Context, id, etag, d string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sheets[id].Description = d
	return nil
}

func (f *fakeBC) DeleteLabelWorksheet(_ context.Context, id, etag string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	delete(f.sheets, id)
	return nil
}

func (f *fakeBC) GetLabelWorksheetLine(_ context.Context, id string) (*vantage.LabelWorksheetLine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.lines[id]
	if !ok {
		return nil, vantage.ErrLabelNotFound
	}
	cp := *l
	return &cp, nil
}

func (f *fakeBC) UpdateLabelWorksheetLineQty(_ context.Context, id, etag string, q int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patched[id] = etag
	f.lines[id].Quantity = q
	return nil
}

func (f *fakeBC) DeleteLabelWorksheetLine(_ context.Context, id, etag string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	delete(f.lines, id)
	return nil
}

func (f *fakeBC) AddLabelScans(_ context.Context, wsID string, scans []vantage.LabelScan) ([]vantage.LabelScanResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse != nil {
		return nil, f.refuse
	}
	f.scans = append(f.scans, scans)
	var out []vantage.LabelScanResult
	for i, s := range scans {
		if s.Barcode == "NOPE" {
			msg := "NOPE is not a barcode of any item."
			out = append(out, vantage.LabelScanResult{ClientScanID: s.ClientScanID, Barcode: s.Barcode, Result: "error", Error: &msg})
			continue
		}
		l := &vantage.LabelWorksheetLine{ID: uuid.NewString(), ETag: `W/"l"`, WorksheetID: wsID, LineNo: (i + 1) * 10000,
			ItemNo: "1896-S", Description: "ATHENS Desk", Quantity: s.Quantity, Barcode: s.Barcode}
		f.lines[l.ID] = l
		res := "added"
		var orig *string
		if s.Barcode == "AGAIN" {
			res = "alreadyProcessed"
			a := "incremented"
			orig = &a
		}
		out = append(out, vantage.LabelScanResult{ClientScanID: s.ClientScanID, Barcode: s.Barcode, Result: res, OriginalResult: orig, Line: l})
	}
	return out, nil
}

type harness struct {
	t      *testing.T
	r      *gin.Engine
	bc     *fakeBC
	tokens map[string]string
}

func (h *harness) do(method, path, who string, body any, hdr ...string) (int, map[string]any, []any) {
	h.t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.tokens[who])
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	var obj map[string]any
	var arr []any
	if strings.HasPrefix(strings.TrimSpace(w.Body.String()), "[") {
		json.Unmarshal(w.Body.Bytes(), &arr)
	} else {
		json.Unmarshal(w.Body.Bytes(), &obj)
	}
	return w.Code, obj, arr
}

func TestLabelPrintingRoutes(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	authSvc := auth.NewService(db, nil, "test-secret-test-secret-test-secret", 10, 3)

	storeID, otherStore := uuid.NewString(), uuid.NewString()
	db.Exec(`INSERT INTO stores (id, store_code, store_name, ls_store_code) VALUES (?, 'S1', 'Spar One', '5002'), (?, 'S2', 'Spar Two', '5003')`, storeID, otherStore)
	printer := auth.Counter{Name: "Mascline", MobileNumber: "+263771000001", CanPrintLabels: true}
	other := auth.Counter{Name: "Tendai", MobileNumber: "+263771000002", CanPrintLabels: true}
	counter := auth.Counter{Name: "Rudo", MobileNumber: "+263771000003"}
	for _, c := range []*auth.Counter{&printer, &other, &counter} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}

	// A label-only person may sign in; a counter on no count may not.
	if ok, _ := auth.CounterMayLogIn(ctx, db, printer.MobileNumber); !ok {
		t.Error("a label printer on no count should be allowed to sign in")
	}
	if ok, _ := auth.CounterMayLogIn(ctx, db, counter.MobileNumber); ok {
		t.Error("a counter on no count and without label printing should not sign in")
	}

	bc := newFakeBC()
	gin.SetMode(gin.TestMode)
	build := func(backend labels.Backend) *gin.Engine {
		r := gin.New()
		counterRoutes := r.Group("",
			middleware.RequireAuth(authSvc, auth.TokenCounter, auth.TokenDevice),
			middleware.ResolveCounter(db),
			middleware.RequireSessionCounter(db))
		labels.NewHandler(db, backend).RegisterCounterRoutes(counterRoutes)
		return r
	}
	tok := func(id string) string { s, _ := authSvc.IssueToken(id, auth.TokenCounter, 1); return s }
	h := &harness{t: t, r: build(bc), bc: bc, tokens: map[string]string{
		"printer": tok(printer.ID), "other": tok(other.ID), "counter": tok(counter.ID)}}

	// /counter/me for both kinds of person.
	if code, me, _ := h.do("GET", "/counter/me", "printer", nil); code != 200 || me["can_print_labels"] != true || me["label_printing_available"] != true {
		t.Fatalf("me (printer): %d %v", code, me)
	}
	if code, me, _ := h.do("GET", "/counter/me", "counter", nil); code != 200 || me["can_print_labels"] != false {
		t.Fatalf("me (counter): %d %v", code, me)
	}
	if code, body, _ := h.do("GET", "/labels/worksheets", "counter", nil); code != 403 || body["code"] != "labels_not_allowed" {
		t.Fatalf("counter without permission: %d %v", code, body)
	}

	// Stores, then a worksheet for Spar One.
	if code, _, stores := h.do("GET", "/labels/stores", "printer", nil); code != 200 || len(stores) != 2 {
		t.Fatalf("stores: %d %v", code, stores)
	}
	code, ws, _ := h.do("POST", "/labels/worksheets", "printer", map[string]any{"store_id": storeID, "description": "Aisle 4"},
		labels.HeaderDeviceName, "TC21-07")
	if code != 201 || ws["store_name"] != "Spar One" || ws["status"] != "Open" || ws["last_printed_at"] != nil {
		t.Fatalf("create: %d %v", code, ws)
	}
	if got := bc.created[0]; got.StoreNo != "5002" || got.OwnerID != printer.ID || got.OwnerName != "Mascline" || got.DeviceID != "TC21-07" {
		t.Fatalf("sent to BC: %+v", got)
	}
	wsID := ws["id"].(string)
	if code, body, _ := h.do("POST", "/labels/worksheets", "printer", map[string]any{"store_id": uuid.NewString()}); code != 400 || body["code"] != "bad_store" {
		t.Fatalf("unknown store: %d %v", code, body)
	}

	// Everyone allowed sees the store's worksheets, whoever made them (BC-made ones too);
	// a store not in the portal stays out. Only the creator may rename or delete.
	bc.sheets["bc-made"] = &vantage.LabelWorksheet{ID: "bc-made", Number: "1111", StoreNo: "5003", Status: "Open", OwnerID: "ADMIN 1"}
	bc.sheets["elsewhere"] = &vantage.LabelWorksheet{ID: "elsewhere", Number: "2222", StoreNo: "9999", Status: "Open", OwnerID: "ADMIN 1"}
	code, _, list := h.do("GET", "/labels/worksheets", "printer", nil)
	if code != 200 || len(list) != 2 {
		t.Fatalf("list: %d %v", code, list)
	}
	first := list[0].(map[string]any)
	if first["number"] != "LW00001" || first["mine"] != true {
		t.Fatalf("own worksheet first (by number): %v", first)
	}
	if bcRow := list[1].(map[string]any); bcRow["owner_name"] != "ADMIN 1" || bcRow["mine"] != false || bcRow["store_name"] != "Spar Two" {
		t.Fatalf("BC-made row: %v", bcRow)
	}
	if code, _, list := h.do("GET", "/labels/worksheets?mine=true", "printer", nil); code != 200 || len(list) != 1 {
		t.Fatalf("mine only: %d %v", code, list)
	}
	if code, _, list := h.do("GET", "/labels/worksheets?store_id="+otherStore, "printer", nil); code != 200 || len(list) != 1 {
		t.Fatalf("one store: %d %v", code, list)
	}
	if code, _, list := h.do("GET", "/labels/worksheets", "other", nil); code != 200 || len(list) != 2 {
		t.Fatalf("other's list: %d %v", code, list)
	}
	if code, _, _ := h.do("GET", "/labels/worksheets/"+wsID, "other", nil); code != 200 {
		t.Fatalf("other reading it: %d, want 200", code)
	}
	if code, _, _ := h.do("GET", "/labels/worksheets/elsewhere", "printer", nil); code != 404 {
		t.Fatalf("worksheet of a store not in the portal: %d, want 404", code)
	}
	if code, body, _ := h.do("PUT", "/labels/worksheets/"+wsID, "other", map[string]any{"description": "x"}); code != 403 || body["code"] != "not_owner" {
		t.Fatalf("other renaming: %d %v", code, body)
	}
	if code, _, _ := h.do("DELETE", "/labels/worksheets/bc-made", "printer", nil); code != 403 {
		t.Fatalf("deleting a BC-made worksheet: %d, want 403", code)
	}

	// Scans: results mapped, scannedBy and device filled in by the server.
	code, res, _ := h.do("POST", "/labels/worksheets/"+wsID+"/scans", "printer", map[string]any{"scans": []map[string]any{
		{"client_scan_id": uuid.NewString(), "barcode": "6001", "quantity": 2, "scanned_at": "2026-11-24T08:00:00Z"},
		{"client_scan_id": uuid.NewString(), "barcode": "NOPE"},
		{"client_scan_id": uuid.NewString(), "barcode": "AGAIN", "quantity": 1},
	}}, labels.HeaderDeviceName, "TC21-07")
	if code != 200 {
		t.Fatalf("scans: %d %v", code, res)
	}
	results := res["results"].([]any)
	r0, r1, r2 := results[0].(map[string]any), results[1].(map[string]any), results[2].(map[string]any)
	if r0["result"] != "added" || r0["line"].(map[string]any)["quantity"] != float64(2) {
		t.Errorf("scan 0: %v", r0)
	}
	if r1["result"] != "error" || !strings.Contains(r1["error"].(string), "NOPE") || r1["line"] != nil {
		t.Errorf("scan 1: %v", r1)
	}
	if r2["result"] != "already_processed" || r2["original_result"] != "incremented" {
		t.Errorf("scan 2: %v", r2)
	}
	sent := bc.scans[0]
	if sent[0].ScannedBy != "Mascline" || sent[0].DeviceID != "TC21-07" || sent[1].Quantity != 1 {
		t.Errorf("scans sent: %+v", sent)
	}
	if code, _, _ := h.do("POST", "/labels/worksheets/bc-made/scans", "other", map[string]any{"scans": []map[string]any{
		{"client_scan_id": uuid.NewString(), "barcode": "6001"}}}); code != 200 {
		t.Errorf("scanning onto a BC-made worksheet: %d, want 200", code)
	}

	// A refusal of the whole call is passed on as 422 with BC's sentence.
	bc.refuse = &vantage.APIError{Status: 400, Message: "Label worksheet LW00001 has been printed."}
	if code, body, _ := h.do("POST", "/labels/worksheets/"+wsID+"/scans", "printer", map[string]any{"scans": []map[string]any{
		{"client_scan_id": uuid.NewString(), "barcode": "6001"}}}); code != 422 || body["error"] != "Label worksheet LW00001 has been printed." {
		t.Errorf("refused: %d %v", code, body)
	}
	bc.refuse = errors.New("dial tcp: no route")
	if code, body, _ := h.do("POST", "/labels/worksheets/"+wsID+"/scans", "printer", map[string]any{"scans": []map[string]any{
		{"client_scan_id": uuid.NewString(), "barcode": "6001"}}}); code != 502 || body["code"] != "vantage_unavailable" {
		t.Errorf("unreachable: %d %v", code, body)
	}
	bc.refuse = nil

	// Lines: the quantity is changed with the line's ETag; a line of another worksheet is refused.
	code, full, _ := h.do("GET", "/labels/worksheets/"+wsID, "printer", nil)
	lines := full["lines"].([]any)
	if code != 200 || len(lines) != 2 {
		t.Fatalf("worksheet with lines: %d %v", code, full)
	}
	lineID := lines[0].(map[string]any)["id"].(string)
	if code, l, _ := h.do("PUT", "/labels/worksheets/"+wsID+"/lines/"+lineID, "printer", map[string]any{"quantity": 5}); code != 200 || l["quantity"] != float64(5) {
		t.Fatalf("qty: %d %v", code, l)
	}
	if bc.patched[lineID] != `W/"l"` {
		t.Errorf("If-Match %q", bc.patched[lineID])
	}
	if code, _, _ := h.do("PUT", "/labels/worksheets/"+wsID+"/lines/"+lineID, "printer", map[string]any{"quantity": 0}); code != 400 {
		t.Errorf("qty 0: %d, want 400", code)
	}
	_, ws2, _ := h.do("POST", "/labels/worksheets", "printer", map[string]any{"store_id": storeID})
	if code, _, _ := h.do("DELETE", "/labels/worksheets/"+ws2["id"].(string)+"/lines/"+lineID, "printer", nil); code != 404 {
		t.Errorf("line through another worksheet: %d, want 404", code)
	}
	if code, _, _ := h.do("DELETE", "/labels/worksheets/"+wsID+"/lines/"+lineID, "printer", nil); code != 200 {
		t.Errorf("delete line: %d", code)
	}

	// A printed worksheet is kept.
	bc.sheets[wsID].Status = "Printed"
	if code, body, _ := h.do("DELETE", "/labels/worksheets/"+wsID, "printer", nil); code != 409 || body["code"] != "worksheet_printed" {
		t.Errorf("delete printed: %d %v", code, body)
	}
	if code, _, list := h.do("GET", "/labels/worksheets?status=printed&mine=true", "printer", nil); code != 200 || len(list) != 1 {
		t.Errorf("printed list: %d %v", code, list)
	}

	// LS Central: no label worksheets.
	h.r = build(nil)
	if code, body, _ := h.do("GET", "/labels/worksheets", "printer", nil); code != 501 || body["code"] != "labels_unsupported" {
		t.Errorf("LS: %d %v", code, body)
	}
	if code, me, _ := h.do("GET", "/counter/me", "printer", nil); code != 200 || me["label_printing_available"] != false {
		t.Errorf("LS me: %d %v", code, me)
	}
}

// A shared device: a label printer on no count can tap in, is on the roster, and the
// device's store is the only one offered.
func TestLabelPrintingOnSharedDevice(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	authSvc := auth.NewService(db, nil, "test-secret-test-secret-test-secret", 10, 3)
	storeID, otherStore := uuid.NewString(), uuid.NewString()
	db.Exec(`INSERT INTO stores (id, store_code, store_name, ls_store_code) VALUES (?, 'S1', 'Spar One', '5002'), (?, 'S2', 'Spar Two', '5003')`, storeID, otherStore)
	admin := uuid.NewString()

	p, err := devices.CreateCounter(ctx, db, "Mascline", "+263771000001", true)
	if err != nil || !p.CanPrintLabels {
		t.Fatalf("create: %+v %v", p, err)
	}
	c, _ := devices.CreateCounter(ctx, db, "Rudo", "+263771000003", false)
	if _, err := devices.CreateCounter(ctx, db, "Mascline M", "+263771000001", true); err != nil {
		t.Fatalf("same number again updates: %v", err)
	}
	if list, _ := devices.ListCounters(ctx, db, "", ""); len(list) != 2 {
		t.Fatalf("counters: %+v", list)
	}
	off := false
	if _, err := devices.UpdateCounter(ctx, db, c.ID, devices.CounterInput{Mobile: strPtr("0771000001")}, auth.NormalizeMobile); !errors.Is(err, devices.ErrMobileInUse) {
		t.Errorf("taking another's number: %v", err)
	}
	if u, err := devices.UpdateCounter(ctx, db, c.ID, devices.CounterInput{CanPrintLabels: &off, Name: strPtr("Rudo M")}, auth.NormalizeMobile); err != nil || u.Name != "Rudo M" {
		t.Errorf("update: %+v %v", u, err)
	}

	dev, _ := devices.Enrol(ctx, db, devices.EnrolRequest{Name: "Zebra #1", InstallID: "install-1", StoreID: &storeID}, admin)
	devices.LinkTag(ctx, db, p.ID, "04A1B2C3D4E5F6", "", admin, false)
	devices.LinkTag(ctx, db, c.ID, "A1B2C3D4", "", admin, false)
	if who, err := devices.Identify(ctx, db, dev, "04A1B2C3D4E5F6"); err != nil || !who.CanPrintLabels || len(who.Sessions) != 0 {
		t.Fatalf("label printer's tap: %+v %v", who, err)
	}
	if _, err := devices.Identify(ctx, db, dev, "A1B2C3D4"); !errors.Is(err, devices.ErrNotOnCount) {
		t.Errorf("counter on nothing: %v", err)
	}
	roster, _ := devices.Roster(ctx, db, dev)
	if len(roster) != 1 || roster[0].CounterID != p.ID || !roster[0].CanPrintLabels || len(roster[0].SessionIDs) != 0 {
		t.Errorf("roster: %+v", roster)
	}

	bc := newFakeBC()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	counterRoutes := r.Group("",
		middleware.RequireAuth(authSvc, auth.TokenCounter, auth.TokenDevice),
		middleware.ResolveCounter(db),
		middleware.RequireSessionCounter(db))
	labels.NewHandler(db, bc).RegisterCounterRoutes(counterRoutes)
	devTok, _ := authSvc.IssueDeviceToken(dev.ID, dev.TokenVersion, 365)
	h := &harness{t: t, r: r, bc: bc, tokens: map[string]string{"dev": devTok}}

	if code, _, stores := h.do("GET", "/labels/stores", "dev", nil, middleware.HeaderCounterID, p.ID); code != 200 || len(stores) != 1 {
		t.Fatalf("device stores: %d %v", code, stores)
	}
	if code, _, _ := h.do("POST", "/labels/worksheets", "dev", map[string]any{"store_id": otherStore}, middleware.HeaderCounterID, p.ID); code != 400 {
		t.Errorf("other store on a store's device: %d, want 400", code)
	}
	code, ws, _ := h.do("POST", "/labels/worksheets", "dev", map[string]any{"store_id": storeID}, middleware.HeaderCounterID, p.ID,
		labels.HeaderDeviceName, "ignored")
	if code != 201 || bc.created[0].DeviceID != "Zebra #1" || bc.created[0].OwnerID != p.ID {
		t.Fatalf("create on device: %d %v %+v", code, ws, bc.created)
	}
}

func strPtr(s string) *string { return &s }
