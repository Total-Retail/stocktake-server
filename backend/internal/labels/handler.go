// Package labels serves Label Printing to the Vantage Suite app (24 Nov 2026).
//
// A person allowed to print labels (counters.can_print_labels) scans items on the shop
// floor; each scan goes onto their label worksheet in Business Central (Vantage Retail
// 0.1.0.40, LabelWorksheet_API.md). The labels are printed from Business Central, as
// before — the app only creates worksheets, scans onto them and adjusts quantities.
//
// Only a Vantage deployment has label worksheets; on LS Central every label route
// answers 501 labels_unsupported.
//
// Access (24 Nov 2026, changed the same day): a person sees and scans onto every
// worksheet of the stores they may use (all active stores with a BC store number; on a
// shared device tied to a store, that store), whoever created it — worksheets are made in
// Business Central as well as from the app. Only the person who created one from the app
// (BC ownerId = their counter id) may rename or delete it.
package labels

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/totalretail/stocktake/internal/auth"
	"github.com/totalretail/stocktake/internal/devices"
	"github.com/totalretail/stocktake/internal/vantage"
	"gorm.io/gorm"
)

// Backend is the Business Central side (vantage.Client).
type Backend interface {
	ListLabelWorksheets(ctx context.Context, f vantage.LabelWorksheetFilter) ([]vantage.LabelWorksheet, error)
	GetLabelWorksheet(ctx context.Context, id string, withLines bool) (*vantage.LabelWorksheet, error)
	CreateLabelWorksheet(ctx context.Context, in vantage.NewLabelWorksheet) (*vantage.LabelWorksheet, error)
	UpdateLabelWorksheetDescription(ctx context.Context, id, etag, description string) error
	DeleteLabelWorksheet(ctx context.Context, id, etag string) error
	GetLabelWorksheetLine(ctx context.Context, id string) (*vantage.LabelWorksheetLine, error)
	UpdateLabelWorksheetLineQty(ctx context.Context, id, etag string, quantity int) error
	DeleteLabelWorksheetLine(ctx context.Context, id, etag string) error
	AddLabelScans(ctx context.Context, worksheetID string, scans []vantage.LabelScan) ([]vantage.LabelScanResult, error)
}

var _ Backend = (*vantage.Client)(nil)

// HeaderDeviceName names the device on a phone sign-in (a shared device is named by its
// enrolment). Stored on the worksheet and each scan as BC's deviceId.
const HeaderDeviceName = "X-Device-Name"

// MaxScansPerCall bounds one sync batch.
const MaxScansPerCall = 200

type Handler struct {
	db      *gorm.DB
	backend Backend // nil: this deployment has no label worksheets (LS Central)
}

// NewHandler: backend nil when the ERP is not Vantage.
func NewHandler(db *gorm.DB, backend Backend) *Handler {
	return &Handler{db: db, backend: backend}
}

// RegisterCounterRoutes goes on the counter route group (counter token, or a shared
// device's token with the tapped-in counter). /counter/me needs no label permission:
// the app asks it to decide which tiles to show.
func (h *Handler) RegisterCounterRoutes(rg *gin.RouterGroup) {
	rg.GET("/counter/me", h.Me)

	l := rg.Group("/labels", h.requireLabelPrinter)
	l.GET("/stores", h.Stores)
	l.GET("/worksheets", h.ListWorksheets)
	l.POST("/worksheets", h.CreateWorksheet)
	l.GET("/worksheets/:worksheet_id", h.GetWorksheet)
	l.PUT("/worksheets/:worksheet_id", h.UpdateWorksheet)
	l.DELETE("/worksheets/:worksheet_id", h.DeleteWorksheet)
	l.POST("/worksheets/:worksheet_id/scans", h.AddScans)
	l.PUT("/worksheets/:worksheet_id/lines/:line_id", h.UpdateLine)
	l.DELETE("/worksheets/:worksheet_id/lines/:line_id", h.DeleteLine)
}

// ── Who is asking ───────────────────────────────────────────────────────────

type person struct {
	ID             string
	Name           string
	MobileNumber   string
	CanPrintLabels bool
}

func (h *Handler) person(c *gin.Context) (*person, error) {
	var rows []person
	err := h.db.WithContext(c.Request.Context()).Raw(`
		SELECT id, name, mobile_number, can_print_labels FROM counters WHERE id::text = ?`,
		c.GetString("user_id")).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errNotYours
	}
	return &rows[0], nil
}

// sharedDevice is the shared device the request came from, nil on a phone sign-in.
func sharedDevice(c *gin.Context) *devices.Device {
	if v, ok := c.Get(devices.CtxDevice); ok {
		if d, ok := v.(*devices.Device); ok {
			return d
		}
	}
	return nil
}

// deviceName is what BC records as deviceId: the shared device's name, else what the
// app sends in X-Device-Name.
func deviceName(c *gin.Context) string {
	if d := sharedDevice(c); d != nil {
		return clip(d.Name, 100)
	}
	return clip(strings.TrimSpace(c.GetHeader(HeaderDeviceName)), 100)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func (h *Handler) requireLabelPrinter(c *gin.Context) {
	if h.backend == nil {
		c.AbortWithStatusJSON(http.StatusNotImplemented, gin.H{
			"error": "Label printing needs a Vantage (Business Central) server; this server uses LS Central.",
			"code":  "labels_unsupported"})
		return
	}
	ok, err := auth.CounterCanPrintLabels(c.Request.Context(), h.db, c.GetString("user_id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "could not check your permissions"})
		return
	}
	if !ok {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "You are not set up for label printing; ask your supervisor.",
			"code":  "labels_not_allowed"})
		return
	}
	c.Next()
}

// Me answers GET /counter/me: who is signed in and what the app may show them.
func (h *Handler) Me(c *gin.Context) {
	p, err := h.person(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	var n int64
	if err := h.db.WithContext(c.Request.Context()).Raw(`
		SELECT COUNT(*) FROM session_counters sc
		JOIN stock_count_sessions s ON s.id = sc.session_id
		WHERE sc.counter_id = ? AND sc.active AND s.status IN ('ACTIVE','REOPENED')`, p.ID).Scan(&n).Error; err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"counter_id":               p.ID,
		"name":                     p.Name,
		"mobile_number":            p.MobileNumber,
		"can_print_labels":         p.CanPrintLabels,
		"label_printing_available": h.backend != nil,
		"open_stock_counts":        n,
	})
}

// ── Stores ──────────────────────────────────────────────────────────────────

// StoreView is a store a worksheet can be made for. VantageStoreNo is the BC store.
type StoreView struct {
	ID             string `json:"id"`
	StoreCode      string `json:"store_code"`
	StoreName      string `json:"store_name"`
	VantageStoreNo string `json:"vantage_store_no"`
}

// stores returns the active stores with a Business Central store number; on a shared
// device tied to a store, only that store.
func (h *Handler) stores(c *gin.Context) ([]StoreView, error) {
	sql := `SELECT id, store_code, store_name, ls_store_code AS vantage_store_no
		FROM stores WHERE active AND ls_store_code <> ''`
	var args []interface{}
	if d := sharedDevice(c); d != nil && d.StoreID != nil {
		sql += ` AND id = ?`
		args = append(args, *d.StoreID)
	}
	sql += ` ORDER BY store_name`
	var out []StoreView
	err := h.db.WithContext(c.Request.Context()).Raw(sql, args...).Scan(&out).Error
	if out == nil {
		out = []StoreView{}
	}
	return out, err
}

func (h *Handler) Stores(c *gin.Context) {
	out, err := h.stores(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func storeNames(list []StoreView) map[string]string {
	m := map[string]string{}
	for _, s := range list {
		m[s.VantageStoreNo] = s.StoreName
	}
	return m
}

// ── Worksheets ──────────────────────────────────────────────────────────────

// WorksheetView is a worksheet as the app shows it. Lines only on GET of one worksheet.
type WorksheetView struct {
	ID          string `json:"id"`
	Number      string `json:"number"`
	StoreNo     string `json:"store_no"`
	StoreName   string `json:"store_name"`
	Description string `json:"description"`
	Status      string `json:"status"` // Open | Printed
	OwnerName   string `json:"owner_name"`
	// Mine: created from the app by the caller (they may rename and delete it).
	Mine           bool       `json:"mine"`
	DeviceName     string     `json:"device_name"`
	CreatedAt      *string    `json:"created_at"`
	LastModifiedAt *string    `json:"last_modified_at"`
	LastPrintedAt  *string    `json:"last_printed_at"`
	LastPrintedBy  string     `json:"last_printed_by"`
	NoOfLines      int        `json:"no_of_lines"`
	TotalLabels    int        `json:"total_labels"`
	LabelsPrinted  int        `json:"labels_printed"`
	Lines          []LineView `json:"lines,omitempty"`
}

// LineView is a worksheet line as the app shows it.
type LineView struct {
	ID                string  `json:"id"`
	LineNo            int     `json:"line_no"`
	Barcode           string  `json:"barcode"`
	ItemNo            string  `json:"item_no"`
	VariantCode       string  `json:"variant_code"`
	UnitOfMeasureCode string  `json:"unit_of_measure_code"`
	Description       string  `json:"description"`
	UnitPrice         float64 `json:"unit_price"`
	CurrencyCode      string  `json:"currency_code"`
	PromotionText     string  `json:"promotion_text"`
	LabelFormatCode   string  `json:"label_format_code"`
	Quantity          int     `json:"quantity"`
	PrintedQty        int     `json:"printed_qty"`
	LastScannedAt     *string `json:"last_scanned_at"`
	ScannedBy         string  `json:"scanned_by"`
}

// bcTime turns BC's "empty" date-times (0001-01-01…) and "" into nil.
func bcTime(s string) *string {
	if s == "" || strings.HasPrefix(s, "0001-01-01") {
		return nil
	}
	return &s
}

func bcTimePtr(s *string) *string {
	if s == nil {
		return nil
	}
	return bcTime(*s)
}

func lineView(l vantage.LabelWorksheetLine) LineView {
	return LineView{
		ID: l.ID, LineNo: l.LineNo, Barcode: l.Barcode, ItemNo: l.ItemNo, VariantCode: l.VariantCode,
		UnitOfMeasureCode: l.UnitOfMeasureCode, Description: l.Description, UnitPrice: l.UnitPrice,
		CurrencyCode: l.CurrencyCode, PromotionText: l.PromotionText, LabelFormatCode: l.LabelFormatCode,
		Quantity: l.Quantity, PrintedQty: l.PrintedQty, LastScannedAt: bcTimePtr(l.LastScannedAt),
		ScannedBy: l.ScannedBy,
	}
}

func worksheetView(w vantage.LabelWorksheet, names map[string]string, withLines bool, caller string) WorksheetView {
	owner := w.OwnerName
	if owner == "" {
		owner = w.OwnerID
	}
	v := WorksheetView{
		ID: w.ID, Number: w.Number, StoreNo: w.StoreNo, StoreName: names[w.StoreNo],
		Description: w.Description, Status: w.Status, OwnerName: owner, Mine: w.OwnerID == caller,
		DeviceName: w.DeviceID,
		CreatedAt:  bcTime(w.CreatedAt), LastModifiedAt: bcTime(w.LastModifiedAt),
		LastPrintedAt: bcTime(w.LastPrintedAt), LastPrintedBy: w.LastPrintedBy,
		NoOfLines: w.NoOfLines, TotalLabels: w.TotalLabels, LabelsPrinted: w.LabelsPrinted,
	}
	if withLines {
		v.Lines = make([]LineView, 0, len(w.Lines))
		for _, l := range w.Lines {
			v.Lines = append(v.Lines, lineView(l))
		}
	}
	return v
}

// ListWorksheets answers GET /labels/worksheets?status=open|printed|all&store_id=…&mine=true:
// the worksheets of the stores the caller may use (or of one of them), whoever made them;
// with mine=true only the caller's own. Newest first. Default status open.
func (h *Handler) ListWorksheets(c *gin.Context) {
	caller := c.GetString("user_id")
	list, err := h.stores(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	var f vantage.LabelWorksheetFilter
	switch strings.ToLower(c.DefaultQuery("status", "open")) {
	case "open":
		f.Status = "Open"
	case "printed":
		f.Status = "Printed"
	case "all":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be open, printed or all"})
		return
	}
	if c.Query("mine") == "true" {
		f.OwnerID = caller
	}
	allowed := storeNames(list)
	if sid := c.Query("store_id"); sid != "" {
		one := map[string]string{}
		for _, s := range list {
			if s.ID == sid {
				one[s.VantageStoreNo] = s.StoreName
			}
		}
		allowed = one
	}
	if len(allowed) == 0 {
		c.JSON(http.StatusOK, []WorksheetView{})
		return
	}
	if len(allowed) == 1 {
		for no := range allowed {
			f.StoreNo = no
		}
	}
	ws, err := h.backend.ListLabelWorksheets(c.Request.Context(), f)
	if err != nil {
		h.fail(c, err)
		return
	}
	names := storeNames(list)
	out := make([]WorksheetView, 0, len(ws))
	for _, w := range ws {
		if _, ok := allowed[w.StoreNo]; ok {
			out = append(out, worksheetView(w, names, false, caller))
		}
	}
	// Newest first: BC numbers worksheets in order, so the number sorts them.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Number > out[j].Number })
	c.JSON(http.StatusOK, out)
}

// CreateWorksheet answers POST /labels/worksheets {store_id, description}.
func (h *Handler) CreateWorksheet(c *gin.Context) {
	var req struct {
		StoreID     string `json:"store_id" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := h.person(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	list, err := h.stores(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	var store *StoreView
	for i := range list {
		if list[i].ID == req.StoreID {
			store = &list[i]
		}
	}
	if store == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Pick one of the stores offered: that store is unknown, inactive, has no Business Central store number, or is not this device's store.",
			"code": "bad_store"})
		return
	}
	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		desc = "Labels " + time.Now().Format("02 Jan 15:04")
	}
	ws, err := h.backend.CreateLabelWorksheet(c.Request.Context(), vantage.NewLabelWorksheet{
		StoreNo: store.VantageStoreNo, Description: clip(desc, 100),
		OwnerID: p.ID, OwnerName: clip(p.Name, 100), DeviceID: deviceName(c),
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, worksheetView(*ws, storeNames(list), true, p.ID))
}

// accessible reads the worksheet and refuses (404) one of a store the caller may not use.
func (h *Handler) accessible(c *gin.Context, withLines bool) (*vantage.LabelWorksheet, error) {
	ws, err := h.backend.GetLabelWorksheet(c.Request.Context(), c.Param("worksheet_id"), withLines)
	if err != nil {
		return nil, err
	}
	list, err := h.stores(c)
	if err != nil {
		return nil, err
	}
	if _, ok := storeNames(list)[ws.StoreNo]; !ok {
		return nil, errNotYours
	}
	return ws, nil
}

// owned is accessible, and refuses (403) a worksheet the caller did not create from the app.
func (h *Handler) owned(c *gin.Context) (*vantage.LabelWorksheet, error) {
	ws, err := h.accessible(c, false)
	if err != nil {
		return nil, err
	}
	if ws.OwnerID != c.GetString("user_id") {
		return nil, errNotOwner
	}
	return ws, nil
}

func (h *Handler) GetWorksheet(c *gin.Context) {
	ws, err := h.accessible(c, true)
	if err != nil {
		h.fail(c, err)
		return
	}
	list, _ := h.stores(c)
	c.JSON(http.StatusOK, worksheetView(*ws, storeNames(list), true, c.GetString("user_id")))
}

// UpdateWorksheet answers PUT /labels/worksheets/:id {description} — the creator only.
func (h *Handler) UpdateWorksheet(c *gin.Context) {
	var req struct {
		Description string `json:"description" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ws, err := h.owned(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	if err := h.backend.UpdateLabelWorksheetDescription(c.Request.Context(), ws.ID, ws.ETag,
		clip(strings.TrimSpace(req.Description), 100)); err != nil {
		h.fail(c, err)
		return
	}
	h.GetWorksheet(c)
}

// DeleteWorksheet answers DELETE /labels/worksheets/:id — the creator only, and only an
// Open worksheet: one printed in Business Central is kept there.
func (h *Handler) DeleteWorksheet(c *gin.Context) {
	ws, err := h.owned(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	if ws.Status != "Open" {
		c.JSON(http.StatusConflict, gin.H{"error": "Worksheet " + ws.Number + " has been printed in Business Central and is kept there.",
			"code": "worksheet_printed"})
		return
	}
	if err := h.backend.DeleteLabelWorksheet(c.Request.Context(), ws.ID, ws.ETag); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ── Scans and lines ─────────────────────────────────────────────────────────

// ScanIn is one scan the app sends. ClientScanID is a GUID the app made once for the
// scan: sending it again (after a lost answer) changes the worksheet once.
type ScanIn struct {
	ClientScanID string `json:"client_scan_id" binding:"required"`
	Barcode      string `json:"barcode" binding:"required"`
	Quantity     int    `json:"quantity"`
	ScannedAt    string `json:"scanned_at"` // RFC 3339; "" = now
}

// ScanOut is what became of one scan. Result: added | incremented | ignored |
// already_processed | error (Error then says why, e.g. unknown barcode).
type ScanOut struct {
	ClientScanID   string    `json:"client_scan_id"`
	Barcode        string    `json:"barcode"`
	Result         string    `json:"result"`
	OriginalResult *string   `json:"original_result"`
	Error          *string   `json:"error"`
	Line           *LineView `json:"line"`
}

var resultNames = map[string]string{
	"added": "added", "incremented": "incremented", "ignored": "ignored",
	"alreadyProcessed": "already_processed", "error": "error",
}

// AddScans answers POST /labels/worksheets/:id/scans {scans: [...]}, in order, at most
// MaxScansPerCall. A refused scan comes back with result error and does not stop the
// rest; a refusal of the whole call (e.g. the worksheet has been printed) is a 422.
func (h *Handler) AddScans(c *gin.Context) {
	var req struct {
		Scans []ScanIn `json:"scans" binding:"required,dive"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Scans) == 0 {
		c.JSON(http.StatusOK, gin.H{"results": []ScanOut{}})
		return
	}
	if len(req.Scans) > MaxScansPerCall {
		c.JSON(http.StatusBadRequest, gin.H{"error": "send at most 200 scans at a time"})
		return
	}
	p, err := h.person(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	ws, err := h.accessible(c, false)
	if err != nil {
		h.fail(c, err)
		return
	}
	dev := deviceName(c)
	scans := make([]vantage.LabelScan, 0, len(req.Scans))
	for _, s := range req.Scans {
		q := s.Quantity
		if q == 0 {
			q = 1
		}
		scans = append(scans, vantage.LabelScan{
			ClientScanID: strings.TrimSpace(s.ClientScanID), Barcode: strings.TrimSpace(s.Barcode),
			Quantity: q, ScannedBy: clip(p.Name, 100), ScannedAt: strings.TrimSpace(s.ScannedAt), DeviceID: dev,
		})
	}
	res, err := h.backend.AddLabelScans(c.Request.Context(), ws.ID, scans)
	if err != nil {
		h.fail(c, err)
		return
	}
	out := make([]ScanOut, 0, len(res))
	for _, r := range res {
		o := ScanOut{ClientScanID: r.ClientScanID, Barcode: r.Barcode, Result: resultNames[r.Result], Error: r.Error}
		if o.Result == "" {
			o.Result = r.Result
		}
		if r.OriginalResult != nil {
			n := resultNames[*r.OriginalResult]
			if n == "" {
				n = *r.OriginalResult
			}
			o.OriginalResult = &n
		}
		if r.Line != nil {
			lv := lineView(*r.Line)
			o.Line = &lv
		}
		out = append(out, o)
	}
	c.JSON(http.StatusOK, gin.H{"results": out})
}

// ownLine reads the line and checks it is on the worksheet in the URL, which the caller may use.
func (h *Handler) ownLine(c *gin.Context) (*vantage.LabelWorksheetLine, error) {
	if _, err := h.accessible(c, false); err != nil {
		return nil, err
	}
	l, err := h.backend.GetLabelWorksheetLine(c.Request.Context(), c.Param("line_id"))
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(l.WorksheetID, c.Param("worksheet_id")) {
		return nil, errNotYours
	}
	return l, nil
}

// UpdateLine answers PUT /labels/worksheets/:id/lines/:line_id {quantity}.
func (h *Handler) UpdateLine(c *gin.Context) {
	var req struct {
		Quantity int `json:"quantity" binding:"required,min=1,max=9999"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must be a whole number from 1 to 9999"})
		return
	}
	l, err := h.ownLine(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	if err := h.backend.UpdateLabelWorksheetLineQty(c.Request.Context(), l.ID, l.ETag, req.Quantity); err != nil {
		h.fail(c, err)
		return
	}
	l, err = h.backend.GetLabelWorksheetLine(c.Request.Context(), l.ID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, lineView(*l))
}

// DeleteLine answers DELETE /labels/worksheets/:id/lines/:line_id.
func (h *Handler) DeleteLine(c *gin.Context) {
	l, err := h.ownLine(c)
	if err != nil {
		h.fail(c, err)
		return
	}
	if err := h.backend.DeleteLabelWorksheetLine(c.Request.Context(), l.ID, l.ETag); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ── Errors ──────────────────────────────────────────────────────────────────

var (
	errNotYours = errors.New("not yours")
	errNotOwner = errors.New("not the creator")
)

func (h *Handler) fail(c *gin.Context, err error) {
	var apiErr *vantage.APIError
	switch {
	case errors.Is(err, errNotYours), errors.Is(err, vantage.ErrLabelNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "That label worksheet or line was not found; it may have been deleted in Business Central.",
			"code": "not_found"})
	case errors.Is(err, errNotOwner):
		c.JSON(http.StatusForbidden, gin.H{"error": "Only the person who created this worksheet in the app can rename or delete it. Do it in Business Central instead.",
			"code": "not_owner"})
	case errors.As(err, &apiErr):
		// Business Central refused, with a sentence a person can act on.
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": apiErr.Message, "code": "vantage_refused"})
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "Business Central did not answer in time; try again.", "code": "vantage_unavailable"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "Could not reach Business Central: " + err.Error(), "code": "vantage_unavailable"})
	}
}
