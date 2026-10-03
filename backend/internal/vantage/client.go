// Package vantage implements ls.Backend against the Vantage Retail API, a
// Business Central online (SaaS) extension published under
// api/totalretailzw/vantage/v1.0. Authentication is OAuth2 client credentials
// against Microsoft Entra ID.
package vantage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/totalretail/stocktake/internal/ls"
)

const (
	defaultScope = "https://api.businesscentral.dynamics.com/.default"

	// tokenSkew is how long before the token's stated expiry we stop using it.
	tokenSkew = 60 * time.Second

	// maxErrBody is the most of a failed response body put in an error.
	maxErrBody = 512

	// maxPages guards against a server that keeps handing back a nextLink.
	maxPages = 10000
)

// Config holds the Vantage connection settings. TokenURL and APIBaseURL are
// optional overrides (used by tests); when empty they are derived from
// TenantID, Environment and CompanyID.
type Config struct {
	TenantID     string
	Environment  string
	CompanyID    string
	ClientID     string
	ClientSecret string

	TokenURL   string
	APIBaseURL string
}

// Client talks to the Vantage API. It is safe for concurrent use.
type Client struct {
	cfg      Config
	tokenURL string
	apiBase  string
	http     *http.Client
	now      func() time.Time

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

var _ ls.Backend = (*Client)(nil)

// NewClient builds a Vantage client. It does not contact the server.
func NewClient(cfg Config) *Client {
	tokenURL := cfg.TokenURL
	if tokenURL == "" {
		tokenURL = fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token",
			url.PathEscape(cfg.TenantID))
	}
	apiBase := cfg.APIBaseURL
	if apiBase == "" {
		apiBase = fmt.Sprintf(
			"https://api.businesscentral.dynamics.com/v2.0/%s/%s/api/totalretailzw/vantage/v1.0/companies(%s)",
			url.PathEscape(cfg.TenantID), url.PathEscape(cfg.Environment), url.PathEscape(cfg.CompanyID))
	}
	return &Client{
		cfg:      cfg,
		tokenURL: tokenURL,
		apiBase:  strings.TrimRight(apiBase, "/"),
		http:     &http.Client{Timeout: 2 * time.Minute},
		now:      time.Now,
	}
}

// ── Records as the Vantage API returns them ─────────────────────────────────

type retailStore struct {
	ID           string `json:"id"`
	StoreNo      string `json:"storeNo"`
	Name         string `json:"name"`
	StoreType    string `json:"storeType"`
	LocationCode string `json:"locationCode"`
	Blocked      bool   `json:"blocked"`
}

type stockCount struct {
	ID               string `json:"id"`
	Number           string `json:"number"`
	WorksheetSeqNo   int    `json:"worksheetSeqNo"`
	Description      string `json:"description"`
	StoreNo          string `json:"storeNo"`
	LocationCode     string `json:"locationCode"`
	Status           string `json:"status"`
	CountDate        string `json:"countDate"`
	CalculatedAt     string `json:"calculatedAt"`
	NoOfLines        int    `json:"noOfLines"`
	PostedDocumentNo string `json:"postedDocumentNo"`
}

type stockCountLine struct {
	ETag              string  `json:"@odata.etag"`
	ID                string  `json:"id"`
	WorksheetSeqNo    int     `json:"worksheetSeqNo"`
	CountNo           string  `json:"countNo"`
	LineNo            int     `json:"lineNo"`
	ItemNo            string  `json:"itemNo"`
	VariantCode       string  `json:"variantCode"`
	Description       string  `json:"description"`
	Barcode           string  `json:"barcode"`
	UnitOfMeasureCode string  `json:"unitOfMeasureCode"`
	QtyCalculated     float64 `json:"qtyCalculated"`
	UnitCost          float64 `json:"unitCost"`
	CountedQty        float64 `json:"countedQty"`
	Counted           bool    `json:"counted"`
}

// ── ls.Backend ──────────────────────────────────────────────────────────────

// GetLSStores returns the unblocked Vantage retail stores, sorted by store number.
func (c *Client) GetLSStores(ctx context.Context) ([]ls.LSStore, error) {
	var stores []retailStore
	if err := c.getList(ctx, "retailStores", "", &stores); err != nil {
		return nil, fmt.Errorf("fetch stores: %w", err)
	}
	out := make([]ls.LSStore, 0, len(stores))
	for _, s := range stores {
		if s.Blocked {
			continue
		}
		out = append(out, ls.LSStore{Code: s.StoreNo, Name: s.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}

// GetAvailableWorksheets returns the stock counts that are in status Counting.
func (c *Client) GetAvailableWorksheets(ctx context.Context) ([]ls.AvailableWorksheet, error) {
	var counts []stockCount
	filter := "status eq " + odataString("Counting")
	if err := c.getList(ctx, "stockCounts", filter, &counts); err != nil {
		return nil, fmt.Errorf("fetch stock counts: %w", err)
	}
	out := make([]ls.AvailableWorksheet, 0, len(counts))
	for _, sc := range counts {
		out = append(out, ls.AvailableWorksheet{
			WorksheetSeqNo: sc.WorksheetSeqNo,
			Description:    sc.Description,
			StoreNo:        sc.StoreNo,
			NoOfLines:      sc.NoOfLines,
		})
	}
	return out, nil
}

// GetWorksheetLines returns the lines of the stock count with this worksheetSeqNo.
func (c *Client) GetWorksheetLines(ctx context.Context, worksheetSeqNo int) ([]ls.WorksheetLine, error) {
	recs, err := c.fetchLines(ctx, worksheetSeqNo)
	if err != nil {
		return nil, err
	}
	out := make([]ls.WorksheetLine, 0, len(recs))
	for _, r := range recs {
		out = append(out, ls.WorksheetLine{
			WorksheetSeqNo: r.WorksheetSeqNo,
			LineNo:         r.LineNo,
			ItemNo:         r.ItemNo,
			Description:    r.Description,
			Barcode:        r.Barcode,
			UoM:            r.UnitOfMeasureCode,
			TheoreticalQty: r.QtyCalculated,
			UnitCost:       r.UnitCost,
			ETag:           r.ETag,
		})
	}
	return out, nil
}

// GetRetailItems returns nothing on Vantage. The LS backend needs it because an
// LS StoreInvJournal line carries no reliable scan barcode, so the session
// service looks the EAN up on the Retail Item card. Every Vantage
// stockCountLine already carries its barcode, so the line's own value is used.
func (c *Client) GetRetailItems(ctx context.Context, itemNos []string) ([]ls.RetailItemLine, error) {
	return nil, nil
}

// GetSKUCosts returns nothing on Vantage. The LS backend reads Stockkeeping
// Unit costs to get a location-specific cost; Vantage computes unitCost on each
// stockCountLine for the count's own location, so the line's cost is already
// the right one and the session service falls back to it.
func (c *Client) GetSKUCosts(ctx context.Context, locationCode string, itemNos []string) ([]ls.SKULine, error) {
	return nil, nil
}

// PostFinalCounts writes countedQty onto each matching line of the count.
// Line numbers that are not on the count are skipped, as on LS. The first
// failed PATCH stops the run and is returned naming the line number.
func (c *Client) PostFinalCounts(ctx context.Context, worksheetSeqNo int, lines []ls.FinalCountLine) error {
	recs, err := c.fetchLines(ctx, worksheetSeqNo)
	if err != nil {
		return fmt.Errorf("fetch lines for submit: %w", err)
	}
	byLineNo := make(map[int]stockCountLine, len(recs))
	for _, r := range recs {
		byLineNo[r.LineNo] = r
	}

	for _, line := range lines {
		rec, ok := byLineNo[line.LineNo]
		if !ok {
			continue
		}
		body, err := json.Marshal(map[string]float64{"countedQty": line.CountedQty})
		if err != nil {
			return fmt.Errorf("marshal patch body for line %d: %w", line.LineNo, err)
		}
		etag := rec.ETag
		if etag == "" {
			etag = "*"
		}
		endpoint := fmt.Sprintf("%s/stockCountLines(%s)", c.apiBase, url.PathEscape(rec.ID))
		status, respBody, err := c.do(ctx, http.MethodPatch, endpoint, body, map[string]string{
			"If-Match":     etag,
			"Content-Type": "application/json",
		})
		if err != nil {
			return fmt.Errorf("patch line %d: %w", line.LineNo, err)
		}
		if status != http.StatusOK && status != http.StatusNoContent {
			return fmt.Errorf("patch line %d: %w", line.LineNo, httpError(status, respBody))
		}
	}
	return nil
}

// ClearWorksheetLines resets the counted quantities on the stock count with
// this worksheetSeqNo through the bound action resetCounts. Unlike LS it does
// NOT delete lines: on Vantage the lines are the calculated snapshot, and
// resetting countedQty is the deliberate equivalent of a fresh count.
func (c *Client) ClearWorksheetLines(ctx context.Context, worksheetSeqNo int) error {
	var counts []stockCount
	filter := fmt.Sprintf("worksheetSeqNo eq %d", worksheetSeqNo)
	if err := c.getList(ctx, "stockCounts", filter, &counts); err != nil {
		return fmt.Errorf("find stock count %d for reset: %w", worksheetSeqNo, err)
	}
	switch len(counts) {
	case 0:
		return fmt.Errorf("no Vantage stock count has worksheetSeqNo %d", worksheetSeqNo)
	case 1:
	default:
		return fmt.Errorf("%d Vantage stock counts have worksheetSeqNo %d; refusing to reset an ambiguous count",
			len(counts), worksheetSeqNo)
	}

	endpoint := fmt.Sprintf("%s/stockCounts(%s)/Microsoft.NAV.resetCounts", c.apiBase, url.PathEscape(counts[0].ID))
	status, respBody, err := c.do(ctx, http.MethodPost, endpoint, []byte("{}"), map[string]string{
		"Content-Type": "application/json",
	})
	if err != nil {
		return fmt.Errorf("reset stock count %d: %w", worksheetSeqNo, err)
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("reset stock count %d: %w", worksheetSeqNo, httpError(status, respBody))
	}
	return nil
}

// ── Plumbing ────────────────────────────────────────────────────────────────

func (c *Client) fetchLines(ctx context.Context, worksheetSeqNo int) ([]stockCountLine, error) {
	var recs []stockCountLine
	filter := fmt.Sprintf("worksheetSeqNo eq %d", worksheetSeqNo)
	if err := c.getList(ctx, "stockCountLines", filter, &recs); err != nil {
		return nil, fmt.Errorf("fetch stock count lines for worksheet %d: %w", worksheetSeqNo, err)
	}
	return recs, nil
}

// getList GETs an entity set (optionally filtered), follows every
// @odata.nextLink, and appends each page's value array into out (a pointer to
// a slice).
func (c *Client) getList(ctx context.Context, entity, filter string, out any) error {
	endpoint := c.apiBase + "/" + entity
	if filter != "" {
		endpoint += "?$filter=" + queryEscape(filter)
	}

	var all []json.RawMessage
	for page := 0; endpoint != ""; page++ {
		if page >= maxPages {
			return fmt.Errorf("GET %s: gave up after %d pages", entity, maxPages)
		}
		status, body, err := c.do(ctx, http.MethodGet, endpoint, nil, nil)
		if err != nil {
			return fmt.Errorf("GET %s: %w", entity, err)
		}
		if status != http.StatusOK {
			return fmt.Errorf("GET %s: %w", entity, httpError(status, body))
		}
		var pg struct {
			Value    []json.RawMessage `json:"value"`
			NextLink string            `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(body, &pg); err != nil {
			return fmt.Errorf("GET %s: decode: %w", entity, err)
		}
		all = append(all, pg.Value...)
		next, err := resolveNextLink(endpoint, pg.NextLink)
		if err != nil {
			return fmt.Errorf("GET %s: %w", entity, err)
		}
		endpoint = next
	}

	joined, err := json.Marshal(all)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(joined, out); err != nil {
		return fmt.Errorf("GET %s: decode records: %w", entity, err)
	}
	return nil
}

// resolveNextLink resolves an @odata.nextLink against the page it came from
// (BC sends absolute links; a relative one is tolerated) and refuses a link to
// another host, so the bearer token is never sent anywhere but the API host.
func resolveNextLink(current, next string) (string, error) {
	if next == "" {
		return "", nil
	}
	cur, err := url.Parse(current)
	if err != nil {
		return "", fmt.Errorf("parse page URL: %w", err)
	}
	ref, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("parse @odata.nextLink: %w", err)
	}
	abs := cur.ResolveReference(ref)
	if abs.Scheme != cur.Scheme || abs.Host != cur.Host {
		return "", fmt.Errorf("@odata.nextLink points at another host (%s); refusing to follow it", abs.Host)
	}
	return abs.String(), nil
}

// do sends one authenticated request. On a 401 it drops the cached token,
// fetches a fresh one and retries exactly once; a second 401 is returned to
// the caller as a status like any other.
func (c *Client) do(ctx context.Context, method, endpoint string, body []byte, headers map[string]string) (int, []byte, error) {
	for attempt := 0; ; attempt++ {
		token, err := c.token(ctx)
		if err != nil {
			return 0, nil, err
		}
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, rdr)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return 0, nil, err
		}
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return 0, nil, fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			c.invalidateToken(token)
			continue
		}
		return resp.StatusCode, respBody, nil
	}
}

// token returns a cached access token, fetching a new one when there is none
// or it is within tokenSkew of expiring.
func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && c.now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"scope":         {defaultScope},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	issued := c.now()
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request: %w", httpError(resp.StatusCode, body))
	}

	var tr struct {
		AccessToken string          `json:"access_token"`
		ExpiresIn   json.RawMessage `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("token response has no access_token")
	}
	secs, err := parseExpiresIn(tr.ExpiresIn)
	if err != nil {
		return "", fmt.Errorf("token response: %w", err)
	}
	c.accessToken = tr.AccessToken
	c.tokenExpiry = issued.Add(time.Duration(secs)*time.Second - tokenSkew)
	return c.accessToken, nil
}

// invalidateToken drops the cached token if it is still the one that was
// rejected (another goroutine may already have replaced it).
func (c *Client) invalidateToken(rejected string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken == rejected {
		c.accessToken = ""
		c.tokenExpiry = time.Time{}
	}
}

// parseExpiresIn accepts expires_in as a JSON number (v2.0 endpoint) or a
// quoted number (the v1.0 endpoint's habit).
func parseExpiresIn(raw json.RawMessage) (int64, error) {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if s == "" {
		return 0, fmt.Errorf("no expires_in")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad expires_in %q", s)
	}
	return n, nil
}

// httpError builds an error carrying the status, BC's error.message (or the
// Entra ID error_description) when present, and up to maxErrBody bytes of the
// raw body.
func httpError(status int, body []byte) error {
	var e struct {
		Error json.RawMessage `json:"error"`
		Desc  string          `json:"error_description"`
	}
	msg := ""
	if json.Unmarshal(body, &e) == nil {
		var bc struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(e.Error, &bc) == nil && bc.Message != "" {
			msg = bc.Message
			if bc.Code != "" {
				msg = bc.Code + ": " + msg
			}
		} else if e.Desc != "" {
			msg = e.Desc
		}
	}
	snippet := body
	if len(snippet) > maxErrBody {
		snippet = snippet[:maxErrBody]
	}
	if msg != "" {
		return fmt.Errorf("Vantage returned %d: %s (body: %s)", status, msg, snippet)
	}
	return fmt.Errorf("Vantage returned %d: %s", status, snippet)
}

// odataString renders s as an OData string literal, doubling single quotes.
func odataString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// queryEscape percent-encodes a query value with %20 for spaces rather than
// '+', which OData servers do not all read as a space.
func queryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
