package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/totalretail/stocktake/internal/auth"
	"github.com/totalretail/stocktake/internal/counting"
	"github.com/totalretail/stocktake/internal/devices"
	"github.com/totalretail/stocktake/internal/reporting"
	"github.com/totalretail/stocktake/internal/session"
	"github.com/totalretail/stocktake/internal/testdb"
	"github.com/totalretail/stocktake/internal/ws"
	"github.com/totalretail/stocktake/pkg/middleware"
)

// 13 Oct 2026 (Wyne) — counters without a device of their own tap an NFC card on a shared
// device; their counts carry their counter id, the device and "NFC".
func TestSharedDeviceCountsAreTaggedWithTheTappedInCounter(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	authSvc := auth.NewService(db, nil, "test-secret-test-secret-test-secret", 10, 3)

	storeID, otherStore := uuid.NewString(), uuid.NewString()
	db.Exec(`INSERT INTO stores (id, store_code, store_name, ls_store_code) VALUES (?, 'S1', 'Spar One', 'S1'), (?, 'S2', 'Spar Two', 'S2')`, storeID, otherStore)
	sess := session.Session{ID: uuid.NewString(), StoreID: storeID, StockCountDate: "2026-10-13", Type: "FLOOR",
		Status: session.StatusActive, CreatedBy: uuid.NewString()}
	db.Create(&sess)
	ada := auth.Counter{Name: "Ada", MobileNumber: "+263771000001"}
	ben := auth.Counter{Name: "Ben", MobileNumber: "+263771000002"}
	db.Create(&ada)
	db.Create(&ben)
	db.Create(&session.SessionCounter{SessionID: sess.ID, CounterID: ada.ID, Active: true})
	admin := uuid.NewString()

	// ── Enrol a device for the store.
	dev, err := devices.Enrol(ctx, db, devices.EnrolRequest{Name: "Zebra #1", InstallID: "install-1", StoreID: &storeID}, admin)
	if err != nil {
		t.Fatal(err)
	}
	token, _ := authSvc.IssueDeviceToken(dev.ID, dev.TokenVersion, 365)

	// ── Cards: link, refuse a card already in use, move it with force.
	if _, err := devices.LinkTag(ctx, db, ada.ID, "04:a1:b2:c3:d4:e5:f6", "Card 7", admin, false); err != nil {
		t.Fatal(err)
	}
	var inUse *devices.TagInUseError
	if _, err := devices.LinkTag(ctx, db, ben.ID, "04A1B2C3D4E5F6", "", admin, false); !errors.As(err, &inUse) || inUse.CounterName != "Ada" {
		t.Fatalf("linking Ada's card to Ben: %v, want TagInUseError naming Ada", err)
	}
	if _, err := devices.LinkTag(ctx, db, ben.ID, "04A1B2C3D4E5F6", "", admin, true); err != nil {
		t.Fatalf("moving the card with force: %v", err)
	}
	if owner, _ := devices.ActiveTagOwner(ctx, db, "04A1B2C3D4E5F6"); owner.ID != ben.ID {
		t.Fatalf("card owner after move = %s, want Ben", owner.Name)
	}
	devices.LinkTag(ctx, db, ada.ID, "04A1B2C3D4E5F6", "Card 7", admin, true)
	if _, err := devices.LinkTag(ctx, db, ben.ID, "1234567890", "", admin, false); !errors.Is(err, devices.ErrBadUID) {
		t.Errorf("a decimal reading: %v, want ErrBadUID", err)
	}
	devices.LinkTag(ctx, db, ben.ID, "A1B2C3D4", "", admin, false)
	var activeCards int64
	db.Model(&devices.CounterTag{}).Where("active").Count(&activeCards)
	if activeCards != 2 {
		t.Errorf("%d active cards, want 2 (one each)", activeCards)
	}

	// ── Taps: Ada is on the count; Ben has a card but no count at this store.
	if who, err := devices.Identify(ctx, db, dev, "04 a1 b2 c3 d4 e5 f6"); err != nil || who.CounterID != ada.ID || len(who.Sessions) != 1 {
		t.Fatalf("Ada's tap: %+v %v", who, err)
	}
	if _, err := devices.Identify(ctx, db, dev, "A1B2C3D4"); !errors.Is(err, devices.ErrNotOnCount) {
		t.Errorf("Ben's tap: %v, want ErrNotOnCount", err)
	}
	if _, err := devices.Identify(ctx, db, dev, "DEADBEEF"); !errors.Is(err, devices.ErrTagUnknown) {
		t.Errorf("unknown card: %v, want ErrTagUnknown", err)
	}
	roster, _ := devices.Roster(ctx, db, dev)
	if len(roster) != 1 || roster[0].CounterID != ada.ID || roster[0].TagUID != "04A1B2C3D4E5F6" {
		t.Errorf("roster = %+v, want only Ada with her card", roster)
	}

	// ── The counter routes, as the server mounts them.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	counterRoutes := r.Group("",
		middleware.RequireAuth(authSvc, auth.TokenCounter, auth.TokenDevice),
		middleware.ResolveCounter(db),
		middleware.RequireSessionCounter(db))
	counting.NewHandler(counting.NewService(db), ws.NewHub()).RegisterRoutes(counterRoutes)
	deviceRoutes := r.Group("/device", middleware.RequireAuth(authSvc, auth.TokenDevice), middleware.RequireActiveDevice(db))
	devices.NewHandler(db, authSvc, 365).RegisterDeviceRoutes(deviceRoutes)

	post := func(bearer, counterID string) int {
		body, _ := json.Marshal(counting.CountBatch{Lines: []counting.CountLine{{
			BinID: uuid.NewString(), ItemNo: "1001", Quantity: 3, RoundNo: 0, ClientUUID: uuid.NewString(),
			AuthMethod: "FORGED",
		}}})
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+sess.ID+"/counts", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+bearer)
		if counterID != "" {
			req.Header.Set(middleware.HeaderCounterID, counterID)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if code := post(token, ada.ID); code != http.StatusOK {
		t.Fatalf("Ada on the shared device: %d, want 200", code)
	}
	if code := post(token, ""); code != http.StatusBadRequest {
		t.Errorf("no one tapped in: %d, want 400", code)
	}
	if code := post(token, ben.ID); code != http.StatusForbidden {
		t.Errorf("Ben, not on the count: %d, want 403", code)
	}
	// A device tied to store S1 cannot count another store's session, even for a counter on it.
	elsewhere := session.Session{ID: uuid.NewString(), StoreID: otherStore, StockCountDate: "2026-10-13", Type: "FLOOR",
		Status: session.StatusActive, CreatedBy: uuid.NewString()}
	db.Create(&elsewhere)
	db.Create(&session.SessionCounter{SessionID: elsewhere.ID, CounterID: ada.ID, Active: true})
	{
		body, _ := json.Marshal(counting.CountBatch{Lines: []counting.CountLine{{
			BinID: uuid.NewString(), ItemNo: "1001", Quantity: 1, ClientUUID: uuid.NewString()}}})
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+elsewhere.ID+"/counts", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(middleware.HeaderCounterID, ada.ID)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("another store's session on the S1 device: %d, want 403", w.Code)
		}
	}
	// Renaming the device leaves its store alone.
	if d, _ := devices.UpdateDevice(ctx, db, dev.ID, "Zebra #1b", false, nil); d.StoreID == nil || *d.StoreID != storeID || d.Name != "Zebra #1b" {
		t.Errorf("rename: %+v, want the store kept", d)
	}

	phone, _ := authSvc.IssueToken(ada.ID, auth.TokenCounter, 12)
	if code := post(phone, ben.ID); code != http.StatusOK {
		t.Errorf("Ada on her own phone (header ignored): %d, want 200", code)
	}

	var lines []counting.CountLine
	db.Where("session_id = ?", sess.ID).Order("synced_at").Find(&lines)
	if len(lines) != 2 {
		t.Fatalf("%d lines, want 2", len(lines))
	}
	for _, l := range lines {
		if l.CounterID != ada.ID {
			t.Errorf("line counted by %s, want Ada", l.CounterID)
		}
	}
	var nfc, otp int
	for _, l := range lines {
		switch {
		case l.AuthMethod == devices.AuthNFC && l.DeviceID != nil && *l.DeviceID == dev.ID:
			nfc++
		case l.AuthMethod == devices.AuthOTP && l.DeviceID == nil:
			otp++
		}
	}
	if nfc != 1 || otp != 1 {
		t.Errorf("lines %+v: want one NFC on the device and one OTP without", lines)
	}

	// ── Reports see both.
	rep := reporting.NewService(db)
	act, err := rep.GetDeviceActivity(ctx, sess.ID)
	if err != nil || len(act) != 2 {
		t.Fatalf("device activity %+v %v, want 2 rows", act, err)
	}
	perf, _ := rep.GetCounterPerformance(ctx, sess.ID)
	if len(perf) != 1 || perf[0].AuthMethods != "NFC, OTP" || perf[0].DevicesUsed != 1 {
		t.Errorf("performance %+v: want Ada with NFC, OTP on 1 device", perf)
	}

	// ── The device's own routes.
	get := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if code := get("/device/roster"); code != http.StatusOK {
		t.Errorf("roster: %d", code)
	}

	// ── Re-enrolling the install gives a new token and kills the old one; revoking kills all.
	again, _ := devices.Enrol(ctx, db, devices.EnrolRequest{Name: "Zebra #1", InstallID: "install-1", StoreID: &storeID}, admin)
	if again.ID != dev.ID || again.TokenVersion != dev.TokenVersion+1 {
		t.Errorf("re-enrol: id %s v%d, want same id, v%d", again.ID, again.TokenVersion, dev.TokenVersion+1)
	}
	if code := post(token, ada.ID); code != http.StatusUnauthorized {
		t.Errorf("superseded token: %d, want 401", code)
	}
	token2, _ := authSvc.IssueDeviceToken(again.ID, again.TokenVersion, 365)
	if code := post(token2, ada.ID); code != http.StatusOK {
		t.Errorf("new token: %d, want 200", code)
	}
	devices.RevokeDevice(ctx, db, dev.ID, admin)
	if code := post(token2, ada.ID); code != http.StatusUnauthorized {
		t.Errorf("revoked device: %d, want 401", code)
	}
	if code := get("/device/roster"); code != http.StatusUnauthorized {
		t.Errorf("revoked device roster: %d, want 401", code)
	}
}

func TestNormalizeUID(t *testing.T) {
	for in, want := range map[string]string{
		"04:a1:b2:c3:d4:e5:f6":    "04A1B2C3D4E5F6",
		" 0xDEADBEEF ":            "DEADBEEF",
		"e0-04-01-50-8a-6b-2c-11": "E00401508A6B2C11",
	} {
		if got, err := devices.NormalizeUID(in); err != nil || got != want {
			t.Errorf("NormalizeUID(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "1234567890", "04A1B2C3D4E5FZ", "ABC"} {
		if _, err := devices.NormalizeUID(bad); err == nil {
			t.Errorf("NormalizeUID(%q) accepted", bad)
		}
	}
}
