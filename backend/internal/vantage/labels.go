package vantage

// Label worksheets (24 Nov 2026). The Vantage Suite app scans items on the shop floor
// onto a label worksheet in Business Central; the labels are printed from Business
// Central as before. This file is the server's side of the Vantage Retail 0.1.0.40 API
// (LabelWorksheet_API.md): labelWorksheets, labelWorksheetLines, labelFormats and the
// bound actions addScan/addScans. Printing actions (printLabels, getLabelsPdf,
// markPrinted) are not used: printing stays in Business Central.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// LabelWorksheet is a labelWorksheets record. With $expand the lines come in Lines.
type LabelWorksheet struct {
	ETag           string               `json:"@odata.etag"`
	ID             string               `json:"id"`
	Number         string               `json:"number"`
	StoreNo        string               `json:"storeNo"`
	LocationCode   string               `json:"locationCode"`
	Description    string               `json:"description"`
	Status         string               `json:"status"` // Open | Printed
	OwnerID        string               `json:"ownerId"`
	OwnerName      string               `json:"ownerName"`
	DeviceID       string               `json:"deviceId"`
	CreatedAt      string               `json:"createdAt"`
	LastModifiedAt string               `json:"lastModifiedAt"`
	LastPrintedAt  string               `json:"lastPrintedAt"`
	LastPrintedBy  string               `json:"lastPrintedBy"`
	NoOfLines      int                  `json:"noOfLines"`
	TotalLabels    int                  `json:"totalLabels"`
	LabelsPrinted  int                  `json:"labelsPrinted"`
	Lines          []LabelWorksheetLine `json:"labelWorksheetLines,omitempty"`
}

// LabelWorksheetLine is a labelWorksheetLines record (also the "line" of a scan result,
// where empty dates come as null rather than 0001-01-01).
type LabelWorksheetLine struct {
	ETag                 string  `json:"@odata.etag,omitempty"`
	ID                   string  `json:"id"`
	WorksheetID          string  `json:"worksheetId"`
	WorksheetNo          string  `json:"worksheetNo"`
	LineNo               int     `json:"lineNo"`
	Barcode              string  `json:"barcode"`
	ItemNo               string  `json:"itemNo"`
	VariantCode          string  `json:"variantCode"`
	UnitOfMeasureCode    string  `json:"unitOfMeasureCode"`
	Description          string  `json:"description"`
	UnitPrice            float64 `json:"unitPrice"`
	CurrencyCode         string  `json:"currencyCode"`
	PriceDate            *string `json:"priceDate"`
	PromotionNo          string  `json:"promotionNo"`
	PromotionText        string  `json:"promotionText"`
	PromotionPrice       float64 `json:"promotionPrice"`
	PromotionPriceStated bool    `json:"promotionPriceStated"`
	LabelFormatCode      string  `json:"labelFormatCode"`
	Quantity             int     `json:"quantity"`
	PrintedQty           int     `json:"printedQty"`
	LastScannedAt        *string `json:"lastScannedAt"`
	ScannedBy            string  `json:"scannedBy"`
}

// LabelFormat is a labelFormats record (the label sizes).
type LabelFormat struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Description string `json:"description"`
	Size        string `json:"size"`
	IsDefault   bool   `json:"isDefault"`
}

// NewLabelWorksheet is what POST labelWorksheets takes.
type NewLabelWorksheet struct {
	StoreNo     string `json:"storeNo"`
	Description string `json:"description"`
	OwnerID     string `json:"ownerId"`
	OwnerName   string `json:"ownerName"`
	DeviceID    string `json:"deviceId"`
}

// LabelScan is one scan for addScans. ScannedAt is ISO 8601 or "" (now); DeviceID ""
// means the worksheet's.
type LabelScan struct {
	ClientScanID string `json:"clientScanId"`
	Barcode      string `json:"barcode"`
	Quantity     int    `json:"quantity"`
	ScannedBy    string `json:"scannedBy"`
	ScannedAt    string `json:"scannedAt,omitempty"`
	DeviceID     string `json:"deviceId,omitempty"`
}

// LabelScanResult is one element of addScans' results. Result is added | incremented |
// ignored | alreadyProcessed | error; Error is set only for error.
type LabelScanResult struct {
	ClientScanID   string              `json:"clientScanId"`
	Barcode        string              `json:"barcode"`
	Result         string              `json:"result"`
	OriginalResult *string             `json:"originalResult"`
	WorksheetNo    string              `json:"worksheetNo"`
	ProcessedAt    *string             `json:"processedAt"`
	Line           *LabelWorksheetLine `json:"line"`
	Error          *string             `json:"error"`
}

// APIError is a refusal from Business Central (HTTP 4xx): Message is the sentence BC
// wrote for a person, without the trailing CorrelationId. The label handlers pass it to
// the app as it is.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("Vantage returned %d: %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("Vantage returned %d: %s", e.Status, e.Message)
}

// ErrLabelNotFound is returned when a worksheet or line id is not in Business Central.
var ErrLabelNotFound = errors.New("not found in Business Central")

var correlationRe = regexp.MustCompile(`\s*CorrelationId:\s*[0-9a-fA-F-]+\.?\s*$`)

// labelError turns a non-2xx response into *APIError (4xx with BC's message),
// ErrLabelNotFound (404) or the generic httpError.
func labelError(status int, body []byte) error {
	if status == http.StatusNotFound {
		return ErrLabelNotFound
	}
	if status >= 400 && status < 500 && status != http.StatusUnauthorized && status != http.StatusForbidden {
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
			return &APIError{
				Status:  status,
				Code:    e.Error.Code,
				Message: strings.TrimSpace(correlationRe.ReplaceAllString(e.Error.Message, "")),
			}
		}
	}
	return httpError(status, body)
}

// guid accepts only a GUID as a key, so an id from a request can never change the URL's
// shape.
var guidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func checkGUID(id string) error {
	if !guidRe.MatchString(id) {
		return ErrLabelNotFound
	}
	return nil
}

// LabelWorksheetFilter narrows ListLabelWorksheets; empty fields do not filter.
type LabelWorksheetFilter struct {
	StoreNo string
	OwnerID string
	Status  string // Open | Printed
}

// ListLabelWorksheets returns the worksheets matching the filter (without lines).
func (c *Client) ListLabelWorksheets(ctx context.Context, f LabelWorksheetFilter) ([]LabelWorksheet, error) {
	var parts []string
	if f.StoreNo != "" {
		parts = append(parts, "storeNo eq "+odataString(f.StoreNo))
	}
	if f.OwnerID != "" {
		parts = append(parts, "ownerId eq "+odataString(f.OwnerID))
	}
	if f.Status != "" {
		parts = append(parts, "status eq "+odataString(f.Status))
	}
	var out []LabelWorksheet
	if err := c.getList(ctx, "labelWorksheets", strings.Join(parts, " and "), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []LabelWorksheet{}
	}
	return out, nil
}

// GetLabelWorksheet reads one worksheet, with its lines when withLines.
func (c *Client) GetLabelWorksheet(ctx context.Context, id string, withLines bool) (*LabelWorksheet, error) {
	if err := checkGUID(id); err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/labelWorksheets(%s)", c.apiBase, id)
	if withLines {
		endpoint += "?$expand=labelWorksheetLines"
	}
	status, body, err := c.do(ctx, http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("get label worksheet: %w", err)
	}
	if status != http.StatusOK {
		return nil, labelError(status, body)
	}
	var ws LabelWorksheet
	if err := json.Unmarshal(body, &ws); err != nil {
		return nil, fmt.Errorf("decode label worksheet: %w", err)
	}
	if ws.Lines == nil {
		ws.Lines = []LabelWorksheetLine{}
	}
	return &ws, nil
}

// CreateLabelWorksheet POSTs a new worksheet and returns it as BC created it.
func (c *Client) CreateLabelWorksheet(ctx context.Context, in NewLabelWorksheet) (*LabelWorksheet, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	status, resp, err := c.do(ctx, http.MethodPost, c.apiBase+"/labelWorksheets", body,
		map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return nil, fmt.Errorf("create label worksheet: %w", err)
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return nil, labelError(status, resp)
	}
	var ws LabelWorksheet
	if err := json.Unmarshal(resp, &ws); err != nil {
		return nil, fmt.Errorf("decode created label worksheet: %w", err)
	}
	if ws.Lines == nil {
		ws.Lines = []LabelWorksheetLine{}
	}
	return &ws, nil
}

// UpdateLabelWorksheetDescription PATCHes the description (the only field BC lets change).
func (c *Client) UpdateLabelWorksheetDescription(ctx context.Context, id, etag, description string) error {
	return c.patch(ctx, "labelWorksheets", id, etag, map[string]any{"description": description})
}

// DeleteLabelWorksheet deletes a worksheet (BC deletes its lines and logs).
func (c *Client) DeleteLabelWorksheet(ctx context.Context, id, etag string) error {
	return c.delete(ctx, "labelWorksheets", id, etag)
}

// GetLabelWorksheetLine reads one line.
func (c *Client) GetLabelWorksheetLine(ctx context.Context, id string) (*LabelWorksheetLine, error) {
	if err := checkGUID(id); err != nil {
		return nil, err
	}
	status, body, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/labelWorksheetLines(%s)", c.apiBase, id), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("get label worksheet line: %w", err)
	}
	if status != http.StatusOK {
		return nil, labelError(status, body)
	}
	var l LabelWorksheetLine
	if err := json.Unmarshal(body, &l); err != nil {
		return nil, fmt.Errorf("decode label worksheet line: %w", err)
	}
	return &l, nil
}

// UpdateLabelWorksheetLineQty PATCHes a line's quantity of labels.
func (c *Client) UpdateLabelWorksheetLineQty(ctx context.Context, id, etag string, quantity int) error {
	return c.patch(ctx, "labelWorksheetLines", id, etag, map[string]any{"quantity": quantity})
}

// DeleteLabelWorksheetLine deletes a line.
func (c *Client) DeleteLabelWorksheetLine(ctx context.Context, id, etag string) error {
	return c.delete(ctx, "labelWorksheetLines", id, etag)
}

// ListLabelFormats returns the label formats (sizes).
func (c *Client) ListLabelFormats(ctx context.Context) ([]LabelFormat, error) {
	var out []LabelFormat
	if err := c.getList(ctx, "labelFormats", "", &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []LabelFormat{}
	}
	return out, nil
}

// AddLabelScans sends scans to the worksheet in one addScans call. BC processes each on
// its own: a refused scan comes back as a result with Result "error", not as an error of
// the call. A scan already processed (same clientScanId) comes back alreadyProcessed.
func (c *Client) AddLabelScans(ctx context.Context, worksheetID string, scans []LabelScan) ([]LabelScanResult, error) {
	if err := checkGUID(worksheetID); err != nil {
		return nil, err
	}
	if len(scans) == 0 {
		return []LabelScanResult{}, nil
	}
	scansJSON, err := json.Marshal(scans)
	if err != nil {
		return nil, err
	}
	var out struct {
		WorksheetNo string            `json:"worksheetNo"`
		Results     []LabelScanResult `json:"results"`
	}
	if err := c.callAction(ctx, "labelWorksheets", worksheetID, "addScans",
		map[string]any{"scansJson": string(scansJSON)}, &out); err != nil {
		return nil, err
	}
	if len(out.Results) != len(scans) {
		return nil, fmt.Errorf("addScans: sent %d scans, Vantage answered %d results", len(scans), len(out.Results))
	}
	return out.Results, nil
}

// ── Plumbing for label calls ────────────────────────────────────────────────

func (c *Client) patch(ctx context.Context, entity, id, etag string, fields map[string]any) error {
	if err := checkGUID(id); err != nil {
		return err
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	if etag == "" {
		etag = "*"
	}
	status, resp, err := c.do(ctx, http.MethodPatch, fmt.Sprintf("%s/%s(%s)", c.apiBase, entity, id), body,
		map[string]string{"Content-Type": "application/json", "If-Match": etag})
	if err != nil {
		return fmt.Errorf("patch %s: %w", entity, err)
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return labelError(status, resp)
	}
	return nil
}

func (c *Client) delete(ctx context.Context, entity, id, etag string) error {
	if err := checkGUID(id); err != nil {
		return err
	}
	if etag == "" {
		etag = "*"
	}
	status, resp, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("%s/%s(%s)", c.apiBase, entity, id), nil,
		map[string]string{"If-Match": etag})
	if err != nil {
		return fmt.Errorf("delete %s: %w", entity, err)
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return labelError(status, resp)
	}
	return nil
}

// callAction POSTs a bound action and decodes the JSON text it returns into out.
//
// BC wraps an action's returned Text as {"value": "<json text>"} (LabelWorksheet_API.md
// §4.0, not yet confirmed on a tenant), so the value is decoded twice. A body that is
// already the object (no "value" string) is accepted too, in case BC answers that way.
func (c *Client) callAction(ctx context.Context, entity, id, action string, params map[string]any, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/%s(%s)/Microsoft.NAV.%s", c.apiBase, entity, url.PathEscape(id), action)
	status, resp, err := c.do(ctx, http.MethodPost, endpoint, body, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return labelError(status, resp)
	}
	inner := resp
	var wrapped struct {
		Value *string `json:"value"`
	}
	if json.Unmarshal(resp, &wrapped) == nil && wrapped.Value != nil {
		inner = []byte(*wrapped.Value)
	}
	if err := json.Unmarshal(inner, out); err != nil {
		return fmt.Errorf("%s: decode result: %w", action, err)
	}
	return nil
}
