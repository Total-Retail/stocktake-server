package session

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/totalretail/stocktake/internal/auth"
	"github.com/totalretail/stocktake/internal/export"
	"github.com/totalretail/stocktake/internal/ls"
	"github.com/totalretail/stocktake/internal/ws"
	"gorm.io/gorm"
)

// CounterSessionView is a richer session response for the mobile counter app.
type CounterSessionView struct {
	ID             string        `json:"id"`
	StoreID        string        `json:"store_id"`
	StoreName      string        `json:"store_name"`
	StockCountDate string        `json:"stock_count_date"`
	Type           SessionType   `json:"type"`
	Status         SessionStatus `json:"status"`
	BinsTotal      int           `json:"bins_total"`
	BinsComplete   int           `json:"bins_complete"`
	PendingRecount int           `json:"pending_recount"`
}

// BinView is a flattened bin record with area/aisle context for the mobile app.
type BinView struct {
	ID        string `json:"id"`
	AreaCode  string `json:"area_code"`
	AreaName  string `json:"area_name"`
	AisleCode string `json:"aisle_code"`
	AisleName string `json:"aisle_name"`
	BinCode   string `json:"bin_code"`
	BinName   string `json:"bin_name"`
	Barcode   string `json:"barcode"`
	Submitted bool   `json:"submitted"`
}

type Service interface {
	ListSessions(ctx context.Context, storeID string) ([]Session, error)
	GetSession(ctx context.Context, id string) (*Session, error)
	CreateSession(ctx context.Context, s Session) (*Session, error)
	UpdateSession(ctx context.Context, id string, worksheetSeqNo int) (*Session, error)
	UpdateStatus(ctx context.Context, id string, status SessionStatus) error
	AbortSession(ctx context.Context, id, reason, adminID string) error
	ReopenSession(ctx context.Context, id string) error
	AddCounter(ctx context.Context, sessionID, counterID string) error
	RemoveCounter(ctx context.Context, sessionID, counterID string) error
	ValidatePullReady(ctx context.Context, sessionID string) error
	PullTheoretical(ctx context.Context, sessionID string) error
	SubmitToLS(ctx context.Context, sessionID, exportDir string) (exportPath string, err error)
	UpsertCounter(ctx context.Context, name, mobile string) (*auth.Counter, error)
	ListCounters(ctx context.Context, sessionID string) ([]auth.Counter, error)
	GetCounterSessions(ctx context.Context, counterID string) ([]Session, error)
	// Counter-specific methods (used by the mobile app)
	GetCounterSessionViews(ctx context.Context, counterID string) ([]CounterSessionView, error)
	GetCounterSessionView(ctx context.Context, sessionID, counterID string) (*CounterSessionView, error)
	GetSessionBins(ctx context.Context, sessionID, counterID string) ([]BinView, error)
	GetSessionItemByBarcode(ctx context.Context, sessionID, barcode string) (*SessionItem, error)
	GetAvailableWorksheets(ctx context.Context) ([]ls.AvailableWorksheet, error)
	GetLSStores(ctx context.Context) ([]ls.LSStore, error)
}

type service struct {
	db       *gorm.DB
	lsClient *ls.Client
	hub      *ws.Hub
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func worksheetSeqNoFromSession(sess *Session) int {
	if sess.WorksheetNo == nil {
		return 0
	}
	n, err := strconv.Atoi(*sess.WorksheetNo)
	if err != nil {
		return 0
	}
	return n
}

func NewService(db *gorm.DB, lsClient *ls.Client, hub *ws.Hub) Service {
	return &service{db: db, lsClient: lsClient, hub: hub}
}

func (s *service) ListSessions(ctx context.Context, storeID string) ([]Session, error) {
	var sessions []Session
	q := s.db.WithContext(ctx).Order("created_at desc")
	if storeID != "" {
		q = q.Where("store_id = ?", storeID)
	}
	return sessions, q.Find(&sessions).Error
}

func (s *service) GetSession(ctx context.Context, id string) (*Session, error) {
	var sess Session
	return &sess, s.db.WithContext(ctx).First(&sess, "id = ?", id).Error
}

func (s *service) CreateSession(ctx context.Context, sess Session) (*Session, error) {
	if sess.Type != TypePartial {
		var count int64
		s.db.WithContext(ctx).Model(&Session{}).
			Where("store_id = ? AND type = ? AND status IN ?", sess.StoreID, sess.Type,
				[]string{"DRAFT", "ACTIVE", "PENDING_REVIEW"}).
			Count(&count)
		if count > 0 {
			return nil, fmt.Errorf("store already has an active %s stock count session", sess.Type)
		}
	}
	sess.Status = StatusDraft
	if err := s.db.WithContext(ctx).Create(&sess).Error; err != nil {
		return nil, err
	}

	// Auto-pull theoreticals if a worksheet was linked at creation.
	// Run in a goroutine with a background context so the HTTP response returns
	// immediately and the pull isn't cancelled when the request context ends.
	// GetRetailItems now fetches only worksheet items, but the pull still runs in the background
	// to avoid blocking the HTTP response.
	if seqNo := worksheetSeqNoFromSession(&sess); seqNo > 0 {
		sessID := sess.ID
		go func() {
			pullCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := s.pullTheoreticalBySeqNo(pullCtx, sessID, seqNo); err != nil {
				log.Printf("WARN [CreateSession] background auto-pull failed for session %s: %v", sessID, err)
			}
		}()
	}

	return &sess, nil
}

func (s *service) UpdateSession(ctx context.Context, id string, worksheetSeqNo int) (*Session, error) {
	var worksheetNoStr *string
	if worksheetSeqNo > 0 {
		str := strconv.Itoa(worksheetSeqNo)
		worksheetNoStr = &str
	}

	if err := s.db.WithContext(ctx).Model(&Session{}).
		Where("id = ?", id).
		Update("worksheet_no", worksheetNoStr).Error; err != nil {
		return nil, err
	}

	if worksheetSeqNo > 0 {
		if err := s.pullTheoreticalBySeqNo(ctx, id, worksheetSeqNo); err != nil {
			return nil, fmt.Errorf("worksheet updated but theoretical pull failed: %w", err)
		}
	}

	return s.GetSession(ctx, id)
}

func (s *service) UpdateStatus(ctx context.Context, id string, status SessionStatus) error {
	if err := s.db.WithContext(ctx).Model(&Session{}).
		Where("id = ?", id).
		Update("status", status).Error; err != nil {
		return err
	}
	s.hub.Broadcast(id, ws.Event{
		Type:      ws.EventSessionUpdated,
		SessionID: id,
		Payload:   map[string]string{"status": string(status)},
	})
	return nil
}

// AbortSession sets a session to ABORTED with a mandatory reason.
func (s *service) AbortSession(ctx context.Context, id, reason, adminID string) error {
	if reason == "" {
		return fmt.Errorf("abort reason is required")
	}
	if err := s.db.WithContext(ctx).Model(&Session{}).
		Where("id = ? AND status IN ?", id, []string{"DRAFT", "ACTIVE", "PENDING_REVIEW", "REOPENED"}).
		Updates(map[string]interface{}{
			"status":       StatusAborted,
			"abort_reason": reason,
			"aborted_at":   gorm.Expr("NOW()"),
			"aborted_by":   adminID,
		}).Error; err != nil {
		return err
	}
	s.hub.Broadcast(id, ws.Event{
		Type:      ws.EventSessionUpdated,
		SessionID: id,
		Payload:   map[string]string{"status": string(StatusAborted), "reason": reason},
	})
	return nil
}

// ReopenSession reopens a POSTED session (privileged admin only).
// Clears the document number and LS worksheet lines.
func (s *service) ReopenSession(ctx context.Context, id string) error {
	sess, err := s.GetSession(ctx, id)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}
	if sess.Status != StatusPosted {
		return fmt.Errorf("only POSTED sessions can be reopened")
	}

	// Clear LS worksheet lines if a worksheet is linked
	if seqNo := worksheetSeqNoFromSession(sess); seqNo > 0 {
		if err := s.lsClient.ClearWorksheetLines(ctx, seqNo); err != nil {
			return fmt.Errorf("failed to clear LS worksheet lines: %w", err)
		}
	}

	if err := s.db.WithContext(ctx).Model(&Session{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":              StatusReopened,
			"document_number":     nil,
			"document_checked_at": nil,
		}).Error; err != nil {
		return err
	}

	s.hub.Broadcast(id, ws.Event{
		Type:      ws.EventSessionUpdated,
		SessionID: id,
		Payload:   map[string]string{"status": string(StatusReopened)},
	})
	return nil
}

func (s *service) GetAvailableWorksheets(ctx context.Context) ([]ls.AvailableWorksheet, error) {
	return s.lsClient.GetAvailableWorksheets(ctx)
}

func (s *service) GetLSStores(ctx context.Context) ([]ls.LSStore, error) {
	return s.lsClient.GetLSStores(ctx)
}

// ValidatePullReady checks that the session exists and has a worksheet linked.
// Call this synchronously before firing a background pull so the caller gets
// a proper error response immediately.
func (s *service) ValidatePullReady(ctx context.Context, sessionID string) error {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}
	if worksheetSeqNoFromSession(sess) == 0 {
		return fmt.Errorf("no worksheet linked to this session — set a worksheet first")
	}
	return nil
}

func (s *service) PullTheoretical(ctx context.Context, sessionID string) error {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}
	seqNo := worksheetSeqNoFromSession(sess)
	if seqNo == 0 {
		return fmt.Errorf("no worksheet linked to this session — set a worksheet first")
	}
	return s.pullTheoreticalBySeqNo(ctx, sessionID, seqNo)
}

func (s *service) pullTheoreticalBySeqNo(ctx context.Context, sessionID string, worksheetSeqNo int) error {
	log.Printf("INFO [pullTheoretical] session=%s worksheet=%d — starting", sessionID, worksheetSeqNo)

	// 1. Worksheet lines — provides the item list and theoretical quantities.
	lines, err := s.lsClient.GetWorksheetLines(ctx, worksheetSeqNo)
	if err != nil {
		log.Printf("ERROR [pullTheoretical] session=%s — worksheet fetch failed: %v", sessionID, err)
		return fmt.Errorf("fetch LS worksheet: %w", err)
	}
	log.Printf("INFO [pullTheoretical] session=%s — got %d worksheet lines", sessionID, len(lines))

	// Build the item number slice once — reused by both downstream LS calls so
	// they fetch only the items on this worksheet instead of the full catalogue.
	itemNos := make([]string, len(lines))
	for i, l := range lines {
		itemNos[i] = l.ItemNo
	}

	// 2. Retail Item card — provides EAN barcodes (ProductExt_DefaultBarcode_Rec)
	//    and a global average unit cost. Fall back gracefully if unavailable.
	retailItems, riErr := s.lsClient.GetRetailItems(ctx, itemNos)
	if riErr != nil {
		log.Printf("WARN [pullTheoretical] session=%s — retail items unavailable: %v — using worksheet barcodes/costs", sessionID, riErr)
	} else {
		log.Printf("INFO [pullTheoretical] session=%s — got %d retail items", sessionID, len(retailItems))
	}
	retailMap := make(map[string]ls.RetailItemLine, len(retailItems))
	for _, ri := range retailItems {
		retailMap[ri.ItemNo] = ri
	}

	// 3. StockkeepingUnit — provides the store-specific (SKU) unit cost.
	//    Requires the store's LS Location_Code; fall back to global cost if missing.
	skuCosts := make(map[string]float64)
	type storeRow struct{ LocationCode string }
	var st storeRow
	if err := s.db.WithContext(ctx).
		Raw("SELECT location_code FROM stores WHERE id = (SELECT store_id FROM stock_count_sessions WHERE id = ?)", sessionID).
		Scan(&st).Error; err == nil && st.LocationCode != "" {
		log.Printf("INFO [pullTheoretical] session=%s — fetching SKU costs for location=%s", sessionID, st.LocationCode)
		skus, skuErr := s.lsClient.GetSKUCosts(ctx, st.LocationCode, itemNos)
		if skuErr != nil {
			log.Printf("WARN [pullTheoretical] session=%s — SKU costs unavailable for location %s: %v — using global costs", sessionID, st.LocationCode, skuErr)
		} else {
			for _, sku := range skus {
				skuCosts[sku.ItemNo] = sku.UnitCost
			}
			log.Printf("INFO [pullTheoretical] session=%s — got %d SKU cost records for location=%s", sessionID, len(skuCosts), st.LocationCode)
		}
	} else {
		log.Printf("WARN [pullTheoretical] session=%s — no location_code on store, skipping SKU costs (err=%v locationCode=%q)", sessionID, err, st.LocationCode)
	}

	// 4. Persist — clear old records and write fresh ones.
	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx.Where("session_id = ?", sessionID).Delete(&TheoreticalStock{})
		tx.Where("session_id = ?", sessionID).Delete(&SessionItem{})

		for _, line := range lines {
			// Barcode: prefer RetailItem EAN barcode; fall back to whatever the worksheet has.
			barcode := line.Barcode
			if ri, ok := retailMap[line.ItemNo]; ok && ri.Barcode != "" {
				barcode = ri.Barcode
			}

			// Cost priority: SKU (location-specific) > RetailItem global > worksheet line cost.
			unitCost := line.UnitCost
			if ri, ok := retailMap[line.ItemNo]; ok && ri.UnitCost > 0 {
				unitCost = ri.UnitCost
			}
			if skuCost, ok := skuCosts[line.ItemNo]; ok && skuCost > 0 {
				unitCost = skuCost
			}

			if err := tx.Create(&TheoreticalStock{
				SessionID:      sessionID,
				ItemNo:         line.ItemNo,
				TheoreticalQty: line.TheoreticalQty,
				UnitCost:       unitCost,
			}).Error; err != nil {
				return err
			}
			if err := tx.Create(&SessionItem{
				SessionID:   sessionID,
				ItemNo:      line.ItemNo,
				Description: line.Description,
				Barcode:     barcode,
				UoM:         line.UoM,
				UnitCost:    unitCost,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if txErr != nil {
		log.Printf("ERROR [pullTheoretical] session=%s — transaction failed: %v", sessionID, txErr)
	} else {
		log.Printf("INFO [pullTheoretical] session=%s — completed OK, saved %d items", sessionID, len(lines))
	}
	return txErr
}

func (s *service) SubmitToLS(ctx context.Context, sessionID, exportDir string) (string, error) {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("session not found: %w", err)
	}
	seqNo := worksheetSeqNoFromSession(sess)
	if seqNo == 0 {
		return "", fmt.Errorf("no worksheet linked to this session")
	}

	wsLines, err := s.lsClient.GetWorksheetLines(ctx, seqNo)
	if err != nil {
		return "", fmt.Errorf("fetch worksheet lines for submit: %w", err)
	}
	lineNoByItem := make(map[string]int, len(wsLines))
	for _, l := range wsLines {
		lineNoByItem[l.ItemNo] = l.LineNo
	}

	type result struct {
		ItemNo   string
		TotalQty float64
	}
	var results []result
	if err := s.db.WithContext(ctx).Raw(`
		SELECT item_no, COALESCE(SUM(quantity), 0) AS total_qty
		FROM count_lines
		WHERE session_id = ?
		AND round_no = (
			SELECT MAX(round_no) FROM count_lines cl2
			WHERE cl2.session_id = count_lines.session_id AND cl2.item_no = count_lines.item_no
		)
		GROUP BY item_no`, sessionID).Scan(&results).Error; err != nil {
		return "", err
	}

	var finalLines []ls.FinalCountLine
	for _, r := range results {
		lineNo, ok := lineNoByItem[r.ItemNo]
		if !ok {
			continue
		}
		finalLines = append(finalLines, ls.FinalCountLine{
			ItemNo:     r.ItemNo,
			LineNo:     lineNo,
			CountedQty: r.TotalQty,
		})
	}

	if err := s.lsClient.PostFinalCounts(ctx, seqNo, finalLines); err != nil {
		return "", err
	}

	// Look up store name for the export header (raw query avoids import cycle)
	type storeRow struct{ StoreName string }
	var st storeRow
	s.db.WithContext(ctx).Raw("SELECT store_name FROM stores WHERE id = ?", sess.StoreID).Scan(&st)

	// Generate Excel export
	exportPath, err := export.GenerateSessionExport(ctx, s.db, sessionID, st.StoreName, sess.StockCountDate, string(sess.Type), exportDir)
	if err != nil {
		// Non-fatal: update status even if export fails, but log the error
		_ = s.UpdateStatus(ctx, sessionID, StatusPendingReview)
		return "", fmt.Errorf("LS submit succeeded but export failed: %w", err)
	}

	// Persist the export file path on the session
	s.db.WithContext(ctx).Model(&Session{}).Where("id = ?", sessionID).Update("export_file_path", exportPath)

	if err := s.UpdateStatus(ctx, sessionID, StatusPendingReview); err != nil {
		return exportPath, err
	}
	return exportPath, nil
}

func (s *service) UpsertCounter(ctx context.Context, name, mobile string) (*auth.Counter, error) {
	mobile = auth.NormalizeMobile(mobile)
	c := auth.Counter{Name: name, MobileNumber: mobile}
	err := s.db.WithContext(ctx).
		Where(auth.Counter{MobileNumber: mobile}).
		Assign(auth.Counter{Name: name}).
		FirstOrCreate(&c).Error
	return &c, err
}

func (s *service) AddCounter(ctx context.Context, sessionID, counterID string) error {
	sc := SessionCounter{SessionID: sessionID, CounterID: counterID, Active: true}
	return s.db.WithContext(ctx).
		Where(SessionCounter{SessionID: sessionID, CounterID: counterID}).
		Assign(SessionCounter{Active: true}).
		FirstOrCreate(&sc).Error
}

func (s *service) RemoveCounter(ctx context.Context, sessionID, counterID string) error {
	return s.db.WithContext(ctx).Model(&SessionCounter{}).
		Where("session_id = ? AND counter_id = ?", sessionID, counterID).
		Update("active", false).Error
}

func (s *service) ListCounters(ctx context.Context, sessionID string) ([]auth.Counter, error) {
	var counters []auth.Counter
	err := s.db.WithContext(ctx).
		Joins("JOIN session_counters ON session_counters.counter_id = counters.id").
		Where("session_counters.session_id = ? AND session_counters.active = ?", sessionID, true).
		Order("counters.name").
		Find(&counters).Error
	return counters, err
}

func (s *service) GetCounterSessions(ctx context.Context, counterID string) ([]Session, error) {
	var sessions []Session
	err := s.db.WithContext(ctx).
		Joins("JOIN session_counters ON session_counters.session_id = stock_count_sessions.id").
		Where("session_counters.counter_id = ? AND session_counters.active = ? AND stock_count_sessions.status IN ?",
			counterID, true, []SessionStatus{StatusActive, StatusReopened}).
		Order("stock_count_sessions.session_date desc").
		Find(&sessions).Error
	log.Printf("DEV [GetCounterSessions] counterID=%s found=%d err=%v", counterID, len(sessions), err)
	return sessions, err
}

func (s *service) GetCounterSessionViews(ctx context.Context, counterID string) ([]CounterSessionView, error) {
	sessions, err := s.GetCounterSessions(ctx, counterID)
	if err != nil {
		return nil, err
	}
	views := make([]CounterSessionView, 0, len(sessions))
	for _, sess := range sessions {
		view, err := s.buildSessionView(ctx, sess, counterID)
		if err != nil {
			log.Printf("WARN [GetCounterSessionViews] skipping session %s: %v", sess.ID, err)
			continue
		}
		views = append(views, *view)
	}
	return views, nil
}

func (s *service) GetCounterSessionView(ctx context.Context, sessionID, counterID string) (*CounterSessionView, error) {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return s.buildSessionView(ctx, *sess, counterID)
}

func (s *service) buildSessionView(ctx context.Context, sess Session, counterID string) (*CounterSessionView, error) {
	type storeRow struct{ StoreName string }
	var sr storeRow
	s.db.WithContext(ctx).Raw("SELECT store_name FROM stores WHERE id = ?", sess.StoreID).Scan(&sr)

	var binsTotal int64
	s.db.WithContext(ctx).Raw(`
		SELECT COUNT(b.id)
		FROM bins b
		JOIN aisles a ON a.id = b.aisle_id
		JOIN areas ar ON ar.id = a.area_id
		WHERE ar.store_id = ? AND b.active = true`, sess.StoreID).Scan(&binsTotal)

	var binsComplete int64
	s.db.WithContext(ctx).Raw(`
		SELECT COUNT(DISTINCT bin_id)
		FROM bin_submissions
		WHERE session_id = ? AND counter_id = ?`, sess.ID, counterID).Scan(&binsComplete)

	var pendingRecount int64
	s.db.WithContext(ctx).Raw(`
		SELECT COUNT(DISTINCT vf.item_no)
		FROM variance_flags vf
		JOIN session_counters sc ON sc.session_id = vf.session_id AND sc.counter_id = ?
		WHERE vf.session_id = ? AND vf.status = 'PENDING'
		AND NOT EXISTS (
			SELECT 1 FROM count_lines cl
			WHERE cl.session_id = vf.session_id AND cl.item_no = vf.item_no
			AND cl.counter_id = ? AND cl.round_no > 0
		)`, counterID, sess.ID, counterID).Scan(&pendingRecount)

	return &CounterSessionView{
		ID:             sess.ID,
		StoreID:        sess.StoreID,
		StoreName:      sr.StoreName,
		StockCountDate: sess.StockCountDate,
		Type:           sess.Type,
		Status:         sess.Status,
		BinsTotal:      int(binsTotal),
		BinsComplete:   int(binsComplete),
		PendingRecount: int(pendingRecount),
	}, nil
}

func (s *service) GetSessionBins(ctx context.Context, sessionID, counterID string) ([]BinView, error) {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	type rawBin struct {
		ID        string
		AreaCode  string
		AreaName  string
		AisleCode string
		AisleName string
		BinCode   string
		BinName   string
		Barcode   string
	}
	var rawBins []rawBin
	if err := s.db.WithContext(ctx).Raw(`
		SELECT b.id, ar.area_code, ar.area_name, a.aisle_code, a.aisle_name,
		       b.bin_code, b.bin_name, b.barcode
		FROM bins b
		JOIN aisles a ON a.id = b.aisle_id
		JOIN areas ar ON ar.id = a.area_id
		WHERE ar.store_id = ? AND b.active = true
		ORDER BY ar.area_code, a.aisle_code, b.bin_code`, sess.StoreID).Scan(&rawBins).Error; err != nil {
		return nil, err
	}

	var submittedIDs []string
	s.db.WithContext(ctx).Raw(`
		SELECT DISTINCT bin_id FROM bin_submissions
		WHERE session_id = ? AND counter_id = ?`, sessionID, counterID).
		Scan(&submittedIDs)
	submittedSet := make(map[string]bool, len(submittedIDs))
	for _, id := range submittedIDs {
		submittedSet[id] = true
	}

	views := make([]BinView, 0, len(rawBins))
	for _, b := range rawBins {
		views = append(views, BinView{
			ID:        b.ID,
			AreaCode:  b.AreaCode,
			AreaName:  b.AreaName,
			AisleCode: b.AisleCode,
			AisleName: b.AisleName,
			BinCode:   b.BinCode,
			BinName:   b.BinName,
			Barcode:   b.Barcode,
			Submitted: submittedSet[b.ID],
		})
	}
	return views, nil
}

func (s *service) GetSessionItemByBarcode(ctx context.Context, sessionID, barcode string) (*SessionItem, error) {
	var item SessionItem
	err := s.db.WithContext(ctx).
		Where("session_id = ? AND barcode = ?", sessionID, barcode).
		First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}
