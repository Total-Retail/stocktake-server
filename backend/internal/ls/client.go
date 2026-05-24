package ls

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// AvailableWorksheet represents a counting worksheet from LS StoreInvWrkSetup
type AvailableWorksheet struct {
	WorksheetSeqNo int    `json:"worksheet_seq_no"`
	Description    string `json:"description"`
	StoreNo        string `json:"store_no"`
	NoOfLines      int    `json:"no_of_lines"`
}
// WorksheetLine
type WorksheetLine struct {
    WorksheetSeqNo int     `json:"WorksheetSeqNo"`
    LineNo         int     `json:"Line_No"`
    ItemNo         string  `json:"Item_No"`
    Description    string  `json:"Description"`
    Barcode        string  `json:"Barcode"`
    UoM            string  `json:"Unit_of_Measure_Code"`
    TheoreticalQty float64 `json:"Qty_Calculated"`
    UnitCost       float64 `json:"Unit_Cost"`
    ETag           string  `json:"-"`
}

// ItemLine
type ItemLine struct {
    ItemNo      string  `json:"No"`
    Description string  `json:"Description"`
    Barcode     string  `json:"Barcode"`
    UoM         string  `json:"BaseUnitOfMeasure"`
    UnitCost    float64 `json:"Unit_Cost"`
}

// RetailItemLine holds barcode and cost data from the LS Retail Item card.
// ProductExt_DefaultBarcode_Rec is the primary EAN/scan barcode on the retail item.
type RetailItemLine struct {
    ItemNo      string  `json:"No"`
    Description string  `json:"Description"`
    Barcode     string  `json:"ProductExt_DefaultBarcode_Rec"`
    UoM         string  `json:"Base_Unit_of_Measure"`
    UnitCost    float64 `json:"Unit_Cost"`
}

// SKULine holds the location-specific unit cost from StockkeepingUnit.
type SKULine struct {
    ItemNo   string  `json:"Item_No"`
    UnitCost float64 `json:"Unit_Cost"`
}

// LSStore represents a store record from LS
type LSStore struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// FinalCountLine is the payload for PostFinalCounts
type FinalCountLine struct {
	ItemNo     string
	LineNo     int
	CountedQty float64
}

type Client struct {
	baseURL  string
	company  string
	username string
	password string
	http     *http.Client
}

func NewClient(baseURL, company, username, password string) *Client {
	return &Client{
		baseURL:  baseURL,
		company:  company,
		username: username,
		password: password,
		http:     &http.Client{},
	}
}

func (c *Client) oDataURL(entity string) string {
	return fmt.Sprintf("%s/ODataV4/Company('%s')/%s",
		c.baseURL, url.PathEscape(c.company), entity)
}

// GetLSStores derives the store list from StoreInvWrkSetup (always available).
// It returns distinct Store_No values with their Description as the name.
// If a dedicated Store entity is published, it will be preferred.
func (c *Client) GetLSStores(ctx context.Context) ([]LSStore, error) {
	// Try dedicated Store entity first
	if stores, err := c.getLSStoresDirect(ctx); err == nil {
		return stores, nil
	}
	// Fallback: derive from StoreInvWrkSetup distinct Store_No + Description prefix
	return c.getLSStoresFromWorksheets(ctx)
}

func (c *Client) getLSStoresDirect(ctx context.Context) ([]LSStore, error) {
	// Try known entity names for the LSC Store page web service.
	for _, entity := range []string{"LSCStore", "LSCStoreList", "Store", "LSC_Store"} {
		endpoint := fmt.Sprintf("%s?$format=json&$select=No,Name", c.oDataURL(entity))
		fmt.Printf("[LS] trying store entity: %s\n", endpoint)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			fmt.Printf("[LS Central] request build error for %s: %v\n", entity, err)
			continue
		}
		req.SetBasicAuth(c.username, c.password)
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			fmt.Printf("[LS] network error for %s: %v\n", entity, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			body := make([]byte, 256)
			n, _ := resp.Body.Read(body)
			fmt.Printf("[LS] %s returned %d: %s\n", entity, resp.StatusCode, body[:n])
			resp.Body.Close()
			continue
		}

		var raw struct {
			Value []map[string]interface{} `json:"value"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		if len(raw.Value) == 0 {
			continue
		}

		out := make([]LSStore, 0, len(raw.Value))
		for _, m := range raw.Value {
			// BC OData v4 encodes "No." as "No" (period stripped)
			code := jsonStr(m, "No")
			if code == "" {
				code = jsonStr(m, "No_x002E_")
			}
			if code == "" {
				code = jsonStr(m, "Code")
			}
			name := jsonStr(m, "Name")
			if name == "" {
				name = jsonStr(m, "Description")
			}
			if code == "" {
				continue
			}
			// Filter out non-retail locations (Warehouse, Head Office, Web Shop, etc.)
			storeType := jsonStr(m, "Store_Type")
			if storeType != "" && storeType != "Store" {
				continue
			}
			out = append(out, LSStore{Code: code, Name: name})
		}
		return out, nil
	}
	return nil, fmt.Errorf("no LSC Store entity available")
}

// getLSStoresFromWorksheets derives distinct stores from StoreInvWrkSetup.
// The Description field contains "<StoreName> Stock Count - <Department>" so we
// extract the store name prefix before " Stock Count".
func (c *Client) getLSStoresFromWorksheets(ctx context.Context) ([]LSStore, error) {
	endpoint := fmt.Sprintf("%s?$format=json&$select=Store_No,Description",
		c.oDataURL("StoreInvWrkSetup"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch stores from worksheets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		return nil, fmt.Errorf("LS returned %d: %s", resp.StatusCode, body[:n])
	}

	var result struct {
		Value []struct {
			StoreNo     string `json:"Store_No"`
			Description string `json:"Description"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	seen := make(map[string]string)
	for _, v := range result.Value {
		if v.StoreNo == "" {
			continue
		}
		if _, ok := seen[v.StoreNo]; ok {
			continue
		}
		// Extract store name: "Arcadia Stock Count - Bakery" -> "Arcadia"
		name := v.StoreNo
		if idx := strings.Index(v.Description, " Stock Count"); idx > 0 {
			name = strings.TrimSpace(v.Description[:idx])
		}
		seen[v.StoreNo] = name
	}

	out := make([]LSStore, 0, len(seen))
	for code, name := range seen {
		out = append(out, LSStore{Code: code, Name: name})
	}
	// Sort by code for stable ordering
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}

// GetAvailableWorksheets returns all counting worksheets from LS StoreInvWrkSetup
func (c *Client) GetAvailableWorksheets(ctx context.Context) ([]AvailableWorksheet, error) {
	// Filter to Type eq 'Counting' only — other types are transfers, returns, etc.
	// Use raw string concatenation to avoid double-encoding of the filter.
	base := c.oDataURL("StoreInvWrkSetup")
	endpoint := base + "?$filter=Type%20eq%20'Counting'&$format=json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch worksheets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		err := fmt.Errorf("LS returned %d fetching worksheets: %s", resp.StatusCode, body[:n])
		fmt.Printf("[LS][ERROR] worksheets: url=%s status=%d body=%s\n", endpoint, resp.StatusCode, body[:n])
		return nil, err
	}

	var result struct {
		Value []struct {
			WorksheetSeqNo int    `json:"WorksheetSeqNo"`
			Description    string `json:"Description"`
			StoreNo        string `json:"Store_No"`
			NoOfLines      int    `json:"No_of_Lines"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode worksheets: %w", err)
	}

	out := make([]AvailableWorksheet, 0, len(result.Value))
	for _, v := range result.Value {
		out = append(out, AvailableWorksheet{
			WorksheetSeqNo: v.WorksheetSeqNo,
			Description:    v.Description,
			StoreNo:        v.StoreNo,
			NoOfLines:      v.NoOfLines,
		})
	}
	return out, nil
}

// GetWorksheetLines fetches inventory journal lines for a given WorksheetSeqNo
func (c *Client) GetWorksheetLines(ctx context.Context, worksheetSeqNo int) ([]WorksheetLine, error) {
	filter := url.QueryEscape(fmt.Sprintf("WorksheetSeqNo eq %d", worksheetSeqNo))
	endpoint := fmt.Sprintf("%s?$filter=%s&$format=json",
		c.oDataURL("StoreInvJournal"), filter)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LS worksheet lines request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		return nil, fmt.Errorf("LS returned %d fetching worksheet lines: %s", resp.StatusCode, body[:n])
	}

	var raw struct {
		Value []json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode worksheet lines: %w", err)
	}

	out := make([]WorksheetLine, 0, len(raw.Value))
	for _, r := range raw.Value {
		var m map[string]interface{}
		if err := json.Unmarshal(r, &m); err != nil {
			continue
		}
		line := WorksheetLine{
			WorksheetSeqNo: int(jsonFloat(m, "WorksheetSeqNo")),
			LineNo:         int(jsonFloat(m, "Line_No")),
			ItemNo:         jsonStr(m, "Item_No"),
			Description:    jsonStr(m, "Description"),
			Barcode:        jsonStr(m, "Barcode"),
			UoM:            jsonStr(m, "Unit_of_Measure_Code"),
			TheoreticalQty: jsonFloat(m, "Qty_Calculated"),
			UnitCost: jsonFloat(m, "Unit_Cost"),
			ETag:           jsonStr(m, "@odata.etag"),
		}
		out = append(out, line)
	}
	return out, nil
}

// PostFinalCounts patches Qty_Phys_Inventory on each matching journal line in LS
func (c *Client) PostFinalCounts(ctx context.Context, worksheetSeqNo int, lines []FinalCountLine) error {
	existingLines, err := c.GetWorksheetLines(ctx, worksheetSeqNo)
	if err != nil {
		return fmt.Errorf("fetch lines for submit: %w", err)
	}

	etagByLineNo := make(map[int]string, len(existingLines))
	for _, l := range existingLines {
		etagByLineNo[l.LineNo] = l.ETag
	}

	for _, line := range lines {
		etag, ok := etagByLineNo[line.LineNo]
		if !ok {
			continue
		}

		patchURL := fmt.Sprintf(
			"%s(WorksheetSeqNo=%d,Line_No=%d)",
			c.oDataURL("StoreInvJournal"),
			worksheetSeqNo,
			line.LineNo,
		)

		body, err := json.Marshal(map[string]interface{}{
			"Qty_Phys_Inventory": line.CountedQty,
		})
		if err != nil {
			return fmt.Errorf("marshal patch body for line %d: %w", line.LineNo, err)
		}

		patchReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, patchURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create patch request for line %d: %w", line.LineNo, err)
		}
		patchReq.SetBasicAuth(c.username, c.password)
		patchReq.Header.Set("Content-Type", "application/json")
		patchReq.Header.Set("Accept", "application/json")
		if etag != "" {
			patchReq.Header.Set("If-Match", etag)
		} else {
			patchReq.Header.Set("If-Match", "*")
		}

		patchResp, err := c.http.Do(patchReq)
		if err != nil {
			return fmt.Errorf("patch line %d: %w", line.LineNo, err)
		}
		patchResp.Body.Close()

		if patchResp.StatusCode != http.StatusNoContent && patchResp.StatusCode != http.StatusOK {
			return fmt.Errorf("patch line %d: LS returned %d", line.LineNo, patchResp.StatusCode)
		}
	}

	return nil
}

// ClearWorksheetLines deletes all physical inventory journal lines for the given worksheetSeqNo.
// Called when a session is reopened to reset the LS worksheet for a fresh count.
func (c *Client) ClearWorksheetLines(ctx context.Context, worksheetSeqNo int) error {
	lines, err := c.GetWorksheetLines(ctx, worksheetSeqNo)
	if err != nil {
		return fmt.Errorf("fetch lines for clear: %w", err)
	}
	for _, line := range lines {
		deleteURL := fmt.Sprintf(
			"%s(WorksheetSeqNo=%d,Line_No=%d)",
			c.oDataURL("StoreInvJournal"),
			worksheetSeqNo,
			line.LineNo,
		)
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
		if err != nil {
			return fmt.Errorf("build delete request for line %d: %w", line.LineNo, err)
		}
		req.SetBasicAuth(c.username, c.password)
		if line.ETag != "" {
			req.Header.Set("If-Match", line.ETag)
		} else {
			req.Header.Set("If-Match", "*")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("delete line %d: %w", line.LineNo, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			return fmt.Errorf("delete line %d: LS returned %d", line.LineNo, resp.StatusCode)
		}
	}
	return nil
}

// GetRetailItems fetches all retail items from LS using the RetailItem card.
// It returns the EAN barcode (ProductExt_DefaultBarcode_Rec) and the global average
// unit cost. Use GetSKUCosts to override with location-specific costs.
// GetRetailItems fetches retail item data for a specific set of item numbers.
// It builds an OData $filter with OR conditions so only the items on the
// worksheet are fetched — not the entire catalogue (which can be thousands of
// records and cause request timeouts).
func (c *Client) GetRetailItems(ctx context.Context, itemNos []string) ([]RetailItemLine, error) {
	if len(itemNos) == 0 {
		return nil, nil
	}

	// Build: No eq '2996' or No eq '4096' or ...
	parts := make([]string, len(itemNos))
	for i, no := range itemNos {
		parts[i] = fmt.Sprintf("No eq '%s'", strings.ReplaceAll(no, "'", "''"))
	}
	filter := url.QueryEscape(strings.Join(parts, " or "))

	endpoint := fmt.Sprintf("%s?$filter=%s&$format=json&$select=No,Description,ProductExt_DefaultBarcode_Rec,Base_Unit_of_Measure,Unit_Cost",
		c.oDataURL("RetailItem"), filter)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LS retail items request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		return nil, fmt.Errorf("LS returned %d fetching retail items: %s", resp.StatusCode, body[:n])
	}

	var result struct {
		Value []RetailItemLine `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode retail items response: %w", err)
	}
	return result.Value, nil
}

// GetSKUCosts fetches per-location unit costs from StockkeepingUnit for the given
// LS location code and specific item numbers (only items on the worksheet).
func (c *Client) GetSKUCosts(ctx context.Context, locationCode string, itemNos []string) ([]SKULine, error) {
	baseFilter := fmt.Sprintf("Location_Code eq '%s'", locationCode)
	if len(itemNos) > 0 {
		itemParts := make([]string, len(itemNos))
		for i, no := range itemNos {
			itemParts[i] = fmt.Sprintf("Item_No eq '%s'", strings.ReplaceAll(no, "'", "''"))
		}
		baseFilter = fmt.Sprintf("(%s) and (%s)", baseFilter, strings.Join(itemParts, " or "))
	}
	filter := url.QueryEscape(baseFilter)
	endpoint := fmt.Sprintf("%s?$filter=%s&$format=json&$select=Item_No,Unit_Cost",
		c.oDataURL("StockkeepingUnit"), filter)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LS SKU costs request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		return nil, fmt.Errorf("LS returned %d fetching SKU costs for location %s: %s", resp.StatusCode, locationCode, body[:n])
	}

	var result struct {
		Value []SKULine `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode SKU costs response: %w", err)
	}
	return result.Value, nil
}

// GetItems fetches the item master from LS
func (c *Client) GetItems(ctx context.Context) ([]ItemLine, error) {
	endpoint := fmt.Sprintf("%s?$format=json&$select=No,Description,Barcode,BaseUnitOfMeasure,Unit_Cost",
    c.oDataURL("Item"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LS items request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		return nil, fmt.Errorf("LS returned %d fetching items: %s", resp.StatusCode, body[:n])
	}

	var result struct {
		Value []ItemLine `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode items response: %w", err)
	}
	return result.Value, nil
}

// jsonStr safely extracts a string value from a map
func jsonStr(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

// jsonFloat safely extracts a float64 value from a map
func jsonFloat(m map[string]interface{}, key string) float64 {
	v, _ := m[key].(float64)
	return v
}