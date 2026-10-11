package session_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/totalretail/stocktake/internal/ls"
	"github.com/totalretail/stocktake/internal/session"
	"github.com/totalretail/stocktake/internal/testdb"
	"github.com/totalretail/stocktake/internal/ws"
	"gorm.io/gorm"
)

// fakeERP answers PostedDocumentNo from a map and records every call.
type fakeERP struct {
	ls.Backend
	posted      map[int]string
	unsupported bool
	asked       []int
	posts       int
}

func (f *fakeERP) PostedDocumentNo(_ context.Context, seq int) (string, error) {
	f.asked = append(f.asked, seq)
	if f.unsupported {
		return "", ls.ErrPostedDocumentUnsupported
	}
	return f.posted[seq], nil
}

func (f *fakeERP) GetWorksheetLines(context.Context, int) ([]ls.WorksheetLine, error) {
	return nil, nil
}

func (f *fakeERP) PostFinalCounts(context.Context, int, []ls.FinalCountLine) error {
	f.posts++
	return nil
}

func newSession(t *testing.T, db *gorm.DB, status session.SessionStatus, worksheet string) string {
	t.Helper()
	id := uuid.NewString()
	s := session.Session{ID: id, StoreID: uuid.NewString(), StockCountDate: "2026-10-11", Type: session.SessionType("FLOOR"),
		Status: status, WorksheetNo: &worksheet, CreatedBy: uuid.NewString()}
	if err := db.Create(&s).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func load(t *testing.T, db *gorm.DB, id string) session.Session {
	t.Helper()
	var s session.Session
	if err := db.First(&s, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return s
}

// C4/H2 (11 Oct 2026) — a submitted session becomes POSTED with the ERP's document number
// once its journal is posted; one still waiting is only stamped as checked; a session
// that was never submitted is not asked about.
func TestSubmittedSessionsBecomePostedWithTheirDocument(t *testing.T) {
	db := testdb.New(t)
	erp := &fakeERP{posted: map[int]string{1: "PIJ-0001", 3: "PIJ-0003"}}
	svc := session.NewService(db, erp, ws.NewHub())
	posted := newSession(t, db, session.StatusPendingReview, "1")
	waiting := newSession(t, db, session.StatusPendingReview, "2")
	active := newSession(t, db, session.StatusActive, "3")

	n, err := svc.CheckPostedDocuments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("marked %d, want 1", n)
	}
	if s := load(t, db, posted); s.Status != session.StatusPosted || s.DocumentNumber == nil || *s.DocumentNumber != "PIJ-0001" || s.DocumentCheckedAt == nil {
		t.Errorf("posted session: status %s, document %v, checked %v", s.Status, s.DocumentNumber, s.DocumentCheckedAt)
	}
	if s := load(t, db, waiting); s.Status != session.StatusPendingReview || s.DocumentNumber != nil || s.DocumentCheckedAt == nil {
		t.Errorf("waiting session: status %s, document %v, checked %v", s.Status, s.DocumentNumber, s.DocumentCheckedAt)
	}
	if s := load(t, db, active); s.Status != session.StatusActive {
		t.Errorf("active session became %s", s.Status)
	}
	for _, seq := range erp.asked {
		if seq == 3 {
			t.Errorf("asked about worksheet 3, whose session was never submitted")
		}
	}
}

func TestABackendThatCannotTellLeavesItToTheAdmin(t *testing.T) {
	db := testdb.New(t)
	erp := &fakeERP{unsupported: true}
	svc := session.NewService(db, erp, ws.NewHub())
	id := newSession(t, db, session.StatusPendingReview, "7")

	if _, err := svc.CheckPostedDocuments(context.Background()); !errors.Is(err, ls.ErrPostedDocumentUnsupported) {
		t.Fatalf("err = %v, want ErrPostedDocumentUnsupported", err)
	}
	if err := svc.SetDocumentNumber(context.Background(), id, "  "); err == nil {
		t.Error("a blank document number was accepted")
	}
	if err := svc.SetDocumentNumber(context.Background(), id, "IJ-7"); err != nil {
		t.Fatal(err)
	}
	if s := load(t, db, id); s.Status != session.StatusPosted || *s.DocumentNumber != "IJ-7" {
		t.Errorf("after manual entry: %s %v", s.Status, s.DocumentNumber)
	}
	active := newSession(t, db, session.StatusActive, "8")
	if err := svc.SetDocumentNumber(context.Background(), active, "IJ-8"); err == nil {
		t.Error("an ACTIVE session was marked posted")
	}
}

// A draft has nothing to send, and a posted or aborted session must not write over what
// finance has: submit refuses them before touching the ERP.
func TestSubmitRefusesDraftPostedAndAbortedSessions(t *testing.T) {
	db := testdb.New(t)
	erp := &fakeERP{}
	svc := session.NewService(db, erp, ws.NewHub())
	for _, st := range []session.SessionStatus{session.StatusDraft, session.StatusPosted, session.StatusAborted} {
		id := newSession(t, db, st, "5")
		if _, err := svc.SubmitToLS(context.Background(), id, t.TempDir()); err == nil {
			t.Errorf("%s session was submitted", st)
		}
	}
	if erp.posts != 0 {
		t.Errorf("the ERP was written %d time(s)", erp.posts)
	}
	id := newSession(t, db, session.StatusActive, "5")
	if _, err := svc.SubmitToLS(context.Background(), id, t.TempDir()); err != nil {
		t.Fatalf("active session: %v", err)
	}
	if erp.posts != 1 {
		t.Errorf("active session: ERP written %d time(s), want 1", erp.posts)
	}
}
