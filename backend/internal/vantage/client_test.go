package vantage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/totalretail/stocktake/internal/ls"
)

// ── Fake Entra ID + Business Central ────────────────────────────────────────

const companyPath = "/v2.0/tnt/env/api/totalretailzw/vantage/v1.0/companies(c0ffee00-0000-0000-0000-000000000001)"

type recordedReq struct {
	Method      string
	Path        string
	RawQuery    string
	Filter      string // decoded $filter
	Auth        string
	Accept      string
	IfMatch     string
	ContentType string
	Body        string
}

type fakeBC struct {
	t   *testing.T
	srv *httptest.Server

	mu         sync.Mutex
	tokenCalls int
	tokenForms []url.Values
	expiresIn  any // number or string, as Entra sends it
	rejectAll  bool
	rejected   map[string]bool // tokens the API answers 401 to
	pageSize   int             // 0 = no paging

	stores []map[string]any
	counts []map[string]any
	lines  []map[string]any

	patchStatus  map[string]int // line id -> status for its PATCH
	patchErrMsg  string
	actionStatus int
	actionErrMsg string

	reqs []recordedReq
}

func newFake(t *testing.T) *fakeBC {
	f := &fakeBC{t: t, expiresIn: 3599, rejected: map[string]bool{}, patchStatus: map[string]int{}, actionStatus: http.StatusNoContent}
	mux := http.NewServeMux()
	mux.HandleFunc("/tnt/oauth2/v2.0/token", f.token)
	mux.HandleFunc("/v2.0/", f.api)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeBC) client() *Client {
	return NewClient(Config{
		TenantID: "tnt", Environment: "env", CompanyID: "c0ffee00-0000-0000-0000-000000000001",
		ClientID: "the-client", ClientSecret: "the-secret",
		TokenURL:   f.srv.URL + "/tnt/oauth2/v2.0/token",
		APIBaseURL: f.srv.URL + companyPath,
	})
}

func (f *fakeBC) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		f.t.Errorf("token form: %v", err)
	}
	f.mu.Lock()
	f.tokenCalls++
	n := f.tokenCalls
	f.tokenForms = append(f.tokenForms, r.PostForm)
	exp := f.expiresIn
	f.mu.Unlock()
	if r.Method != http.MethodPost {
		f.t.Errorf("token method = %s, want POST", r.Method)
	}
	writeJSON(w, http.StatusOK, map[string]any{"token_type": "Bearer", "access_token": fmt.Sprintf("tok-%d", n), "expires_in": exp})
}

func (f *fakeBC) tokenCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenCalls
}

func (f *fakeBC) requests() []recordedReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedReq(nil), f.reqs...)
}

func (f *fakeBC) requestsByMethod(m string) []recordedReq {
	var out []recordedReq
	for _, r := range f.requests() {
		if r.Method == m {
			out = append(out, r)
		}
	}
	return out
}

var (
	reEq      = regexp.MustCompile(`^(\w+) eq (.+)$`)
	reKeyPath = regexp.MustCompile(`^/(\w+)\(([^)]*)\)(/.*)?$`)
)

func (f *fakeBC) api(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	q := r.URL.Query()
	rec := recordedReq{
		Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery, Filter: q.Get("$filter"),
		Auth: r.Header.Get("Authorization"), Accept: r.Header.Get("Accept"),
		IfMatch: r.Header.Get("If-Match"), ContentType: r.Header.Get("Content-Type"), Body: string(body),
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, rec)
	tok := strings.TrimPrefix(rec.Auth, "Bearer ")
	unauthorized := f.rejectAll || f.rejected[tok] || !strings.HasPrefix(rec.Auth, "Bearer tok-")
	f.mu.Unlock()
	if unauthorized {
		writeJSON(w, http.StatusUnauthorized, bcErr("Authentication_InvalidCredentials", "The credentials provided are incorrect"))
		return
	}

	rest, ok := strings.CutPrefix(r.URL.Path, companyPath)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.Method == http.MethodGet && rest == "/retailStores":
		f.list(w, r, f.stores)
	case r.Method == http.MethodGet && rest == "/stockCounts":
		f.list(w, r, f.counts)
	case r.Method == http.MethodGet && rest == "/stockCountLines":
		f.list(w, r, f.lines)
	case r.Method == http.MethodPatch && strings.HasPrefix(rest, "/stockCountLines("):
		m := reKeyPath.FindStringSubmatch(rest)
		f.mu.Lock()
		st, set := f.patchStatus[m[2]]
		msg := f.patchErrMsg
		f.mu.Unlock()
		if !set {
			st = http.StatusOK
		}
		if st >= 300 {
			writeJSON(w, st, bcErr("Internal_EntityWithSameKeyExists", msg))
			return
		}
		writeJSON(w, st, map[string]any{"id": m[2]})
	case r.Method == http.MethodPost && strings.HasPrefix(rest, "/stockCounts("):
		f.mu.Lock()
		st, msg := f.actionStatus, f.actionErrMsg
		f.mu.Unlock()
		if st >= 300 {
			writeJSON(w, st, bcErr("Application_DialogException", msg))
			return
		}
		w.WriteHeader(st)
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

// list applies the simple "<field> eq <literal>" filters the client sends and
// pages with $skiptoken when pageSize is set, the way BC does.
func (f *fakeBC) list(w http.ResponseWriter, r *http.Request, rows []map[string]any) {
	q := r.URL.Query()
	var out []map[string]any
	filter := q.Get("$filter")
	for _, row := range rows {
		if filter != "" {
			m := reEq.FindStringSubmatch(filter)
			if m == nil {
				writeJSON(w, http.StatusBadRequest, bcErr("BadRequest", "unsupported filter "+filter))
				return
			}
			want := m[2]
			var got string
			switch v := row[m[1]].(type) {
			case string:
				got = "'" + strings.ReplaceAll(v, "'", "''") + "'"
			case int:
				got = strconv.Itoa(v)
			default:
				got = fmt.Sprint(v)
			}
			if got != want {
				continue
			}
		}
		out = append(out, row)
	}
	resp := map[string]any{}
	if f.pageSize > 0 {
		skip, _ := strconv.Atoi(q.Get("$skiptoken"))
		end := skip + f.pageSize
		if end < len(out) {
			nq := url.Values{}
			if filter != "" {
				nq.Set("$filter", filter)
			}
			nq.Set("$skiptoken", strconv.Itoa(end))
			resp["@odata.nextLink"] = f.srv.URL + r.URL.Path + "?" + nq.Encode()
		} else {
			end = len(out)
		}
		if skip > len(out) {
			skip = len(out)
		}
		out = out[skip:end]
	}
	if out == nil {
		out = []map[string]any{}
	}
	resp["value"] = out
	writeJSON(w, http.StatusOK, resp)
}

func bcErr(code, msg string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": msg}}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func line(id string, seq, lineNo int, itemNo, etag string) map[string]any {
	m := map[string]any{
		"id": id, "worksheetSeqNo": seq, "countNo": "SC000007", "lineNo": lineNo, "itemNo": itemNo,
		"variantCode": "", "description": "Item " + itemNo, "barcode": "600" + itemNo, "unitOfMeasureCode": "PCS",
		"qtyCalculated": 12.5, "unitCost": 3.25, "countedQty": 0, "counted": false,
	}
	if etag != "" {
		m["@odata.etag"] = etag
	}
	return m
}

func mustContain(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q, got nil", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error %q does not contain %q", err.Error(), p)
		}
	}
}

var ctx = context.Background()

// ── Construction ────────────────────────────────────────────────────────────

func TestNewClientDefaultURLs(t *testing.T) {
	c := NewClient(Config{TenantID: "t-1", Environment: "Production", CompanyID: "abc-123"})
	if want := "https://login.microsoftonline.com/t-1/oauth2/v2.0/token"; c.tokenURL != want {
		t.Errorf("tokenURL = %q, want %q", c.tokenURL, want)
	}
	if want := "https://api.businesscentral.dynamics.com/v2.0/t-1/Production/api/totalretailzw/vantage/v1.0/companies(abc-123)"; c.apiBase != want {
		t.Errorf("apiBase = %q, want %q", c.apiBase, want)
	}
}

func TestImplementsBackend(t *testing.T) {
	var _ ls.Backend = NewClient(Config{})
}

// ── Token ───────────────────────────────────────────────────────────────────

func TestTokenRequestForm(t *testing.T) {
	f := newFake(t)
	if _, err := f.client().GetLSStores(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.tokenForms) != 1 {
		t.Fatalf("token calls = %d, want 1", len(f.tokenForms))
	}
	want := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"the-client"},
		"client_secret": {"the-secret"},
		"scope":         {"https://api.businesscentral.dynamics.com/.default"},
	}
	if !reflect.DeepEqual(f.tokenForms[0], want) {
		t.Errorf("token form = %v, want %v", f.tokenForms[0], want)
	}
	for _, r := range f.requests() {
		if r.Auth != "Bearer tok-1" || r.Accept != "application/json" {
			t.Errorf("%s %s: Authorization=%q Accept=%q", r.Method, r.Path, r.Auth, r.Accept)
		}
	}
}

func TestTokenCachedAcrossCalls(t *testing.T) {
	f := newFake(t)
	f.counts = []map[string]any{{"id": "g1", "worksheetSeqNo": 7, "status": "Counting"}}
	f.lines = []map[string]any{line("l1", 7, 10000, "1001", "W/\"a\"")}
	c := f.client()
	for i := 0; i < 3; i++ {
		if _, err := c.GetLSStores(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := c.GetAvailableWorksheets(ctx); err != nil {
			t.Fatal(err)
		}
		if err := c.PostFinalCounts(ctx, 7, []ls.FinalCountLine{{LineNo: 10000, CountedQty: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.tokenCount(); got != 1 {
		t.Errorf("token requests = %d for 9 API calls, want 1", got)
	}
}

// expires_in is honoured with a 60 s margin: still cached 61 s before expiry,
// refetched 59 s before it. Entra's v1 habit of a quoted number is accepted.
func TestTokenExpiryMargin(t *testing.T) {
	tests := []struct {
		name       string
		expiresIn  any
		advance    time.Duration
		wantTokens int
	}{
		{"fresh", 3600, 0, 1},
		{"61s before expiry still cached", 3600, 3600*time.Second - 61*time.Second, 1},
		{"59s before expiry refetched", 3600, 3600*time.Second - 59*time.Second, 2},
		{"after expiry refetched", 3600, 2 * time.Hour, 2},
		{"quoted expires_in", "3600", 3600*time.Second - 61*time.Second, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t)
			f.expiresIn = tt.expiresIn
			c := f.client()
			now := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
			c.now = func() time.Time { return now }
			if _, err := c.GetLSStores(ctx); err != nil {
				t.Fatal(err)
			}
			now = now.Add(tt.advance)
			if _, err := c.GetLSStores(ctx); err != nil {
				t.Fatal(err)
			}
			if got := f.tokenCount(); got != tt.wantTokens {
				t.Errorf("token requests = %d, want %d", got, tt.wantTokens)
			}
		})
	}
}

func TestTokenRefreshedOnceOn401(t *testing.T) {
	f := newFake(t)
	f.stores = []map[string]any{{"storeNo": "S1", "name": "One"}}
	f.rejected["tok-1"] = true // e.g. revoked server-side before its expiry
	stores, err := f.client().GetLSStores(ctx)
	if err != nil {
		t.Fatalf("GetLSStores after refresh: %v", err)
	}
	if len(stores) != 1 {
		t.Errorf("stores = %v", stores)
	}
	if got := f.tokenCount(); got != 2 {
		t.Errorf("token requests = %d, want 2", got)
	}
	reqs := f.requests()
	if len(reqs) != 2 || reqs[0].Auth != "Bearer tok-1" || reqs[1].Auth != "Bearer tok-2" {
		t.Errorf("API requests = %+v, want tok-1 then tok-2", reqs)
	}
}

func TestPersistent401FailsAfterOneRetry(t *testing.T) {
	f := newFake(t)
	f.rejectAll = true
	_, err := f.client().GetLSStores(ctx)
	mustContain(t, err, "401", "The credentials provided are incorrect")
	if got := f.tokenCount(); got != 2 {
		t.Errorf("token requests = %d, want 2 (one refresh, then fail)", got)
	}
	if got := len(f.requests()); got != 2 {
		t.Errorf("API requests = %d, want 2", got)
	}
}

func TestTokenEndpointErrorSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":             "invalid_client",
			"error_description": "AADSTS7000215: Invalid client secret provided.",
		})
	}))
	defer srv.Close()
	c := NewClient(Config{TokenURL: srv.URL, APIBaseURL: srv.URL})
	_, err := c.GetLSStores(ctx)
	mustContain(t, err, "401", "AADSTS7000215")
}

// ── Paging ──────────────────────────────────────────────────────────────────

func TestPagingFollowsNextLink(t *testing.T) {
	f := newFake(t)
	f.pageSize = 2
	for i := 5; i >= 1; i-- {
		f.stores = append(f.stores, map[string]any{"storeNo": fmt.Sprintf("S%d", i), "name": fmt.Sprintf("Store %d", i)})
	}
	for i := 1; i <= 5; i++ {
		f.lines = append(f.lines, line(fmt.Sprintf("l%d", i), 7, i*10000, strconv.Itoa(1000+i), ""))
	}
	f.lines = append(f.lines, line("other", 8, 10000, "9999", "")) // another count
	c := f.client()

	stores, err := c.GetLSStores(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stores) != 5 {
		t.Errorf("stores across pages = %d, want 5", len(stores))
	}
	lines, err := c.GetWorksheetLines(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 5 {
		t.Errorf("lines across pages = %d, want 5", len(lines))
	}
	var lineGets []recordedReq
	for _, r := range f.requestsByMethod(http.MethodGet) {
		if strings.HasSuffix(r.Path, "/stockCountLines") {
			lineGets = append(lineGets, r)
		}
	}
	if len(lineGets) != 3 {
		t.Fatalf("stockCountLines GETs = %d, want 3 pages", len(lineGets))
	}
	for _, r := range lineGets {
		if r.Filter != "worksheetSeqNo eq 7" {
			t.Errorf("page request lost its filter: %q", r.RawQuery)
		}
	}
}

func TestNextLinkToAnotherHostRefused(t *testing.T) {
	var foreignHits int
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignHits++
		writeJSON(w, http.StatusOK, map[string]any{"value": []any{}})
	}))
	defer foreign.Close()
	f := newFake(t)
	c := f.client()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"value": []any{}, "@odata.nextLink": foreign.URL + "/steal"})
	}))
	defer api.Close()
	c.apiBase = api.URL
	_, err := c.GetLSStores(ctx)
	mustContain(t, err, "another host")
	if foreignHits != 0 {
		t.Errorf("foreign host received %d requests (bearer token leaked)", foreignHits)
	}
}

// ── Mapping and filters ─────────────────────────────────────────────────────

func TestGetLSStores(t *testing.T) {
	f := newFake(t)
	f.stores = []map[string]any{
		{"id": "g3", "storeNo": "S30", "name": "Borrowdale", "storeType": "Store", "locationCode": "L30", "blocked": false},
		{"id": "g1", "storeNo": "S10", "name": "Avondale", "storeType": "Store", "locationCode": "L10", "blocked": false},
		{"id": "g2", "storeNo": "S20", "name": "Closed Store", "storeType": "Store", "locationCode": "L20", "blocked": true},
		{"id": "g4", "storeNo": "S05", "name": "Eastgate", "storeType": "Store", "locationCode": "L05"},
	}
	got, err := f.client().GetLSStores(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []ls.LSStore{{Code: "S05", Name: "Eastgate"}, {Code: "S10", Name: "Avondale"}, {Code: "S30", Name: "Borrowdale"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stores = %+v, want %+v", got, want)
	}
}

func TestGetAvailableWorksheets(t *testing.T) {
	f := newFake(t)
	f.counts = []map[string]any{
		{"id": "g1", "number": "SC000001", "worksheetSeqNo": 1, "description": "Open one", "storeNo": "S10", "status": "Open", "noOfLines": 1},
		{"id": "g2", "number": "SC000002", "worksheetSeqNo": 2, "description": "Arcadia Stock Count - Bakery", "storeNo": "S20",
			"locationCode": "L20", "status": "Counting", "countDate": "2026-10-01", "calculatedAt": "2026-10-01T06:00:00Z", "noOfLines": 42, "postedDocumentNo": ""},
		{"id": "g3", "number": "SC000003", "worksheetSeqNo": 3, "description": "Posted", "storeNo": "S10", "status": "Posted", "noOfLines": 9, "postedDocumentNo": "PIJ-1"},
		{"id": "g4", "number": "SC000004", "worksheetSeqNo": 4, "description": "Transferred", "storeNo": "S10", "status": "Transferred", "noOfLines": 5},
	}
	got, err := f.client().GetAvailableWorksheets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []ls.AvailableWorksheet{{WorksheetSeqNo: 2, Description: "Arcadia Stock Count - Bakery", StoreNo: "S20", NoOfLines: 42}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("worksheets = %+v, want %+v", got, want)
	}
	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	if reqs[0].Filter != "status eq 'Counting'" {
		t.Errorf("decoded $filter = %q, want %q", reqs[0].Filter, "status eq 'Counting'")
	}
	if strings.ContainsAny(reqs[0].RawQuery, " +'") {
		t.Errorf("raw query not percent-encoded (spaces must be %%20, quotes %%27): %q", reqs[0].RawQuery)
	}
}

func TestGetWorksheetLinesMapsEveryField(t *testing.T) {
	f := newFake(t)
	f.lines = []map[string]any{
		{"@odata.etag": `W/"JzQ0O0VqYz0nOyc="`, "id": "11111111-aaaa-bbbb-cccc-000000000001", "worksheetSeqNo": 7, "countNo": "SC000007",
			"lineNo": 20000, "itemNo": "40012", "variantCode": "RED", "description": "Kettle 1.7L", "barcode": "6001234567890",
			"unitOfMeasureCode": "BOX", "qtyCalculated": 14.5, "unitCost": 23.75, "countedQty": 0, "counted": false},
		line("x", 8, 10000, "9999", ""), // other count — must be filtered out server-side
	}
	got, err := f.client().GetWorksheetLines(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []ls.WorksheetLine{{
		WorksheetSeqNo: 7, LineNo: 20000, ItemNo: "40012", Description: "Kettle 1.7L", Barcode: "6001234567890",
		UoM: "BOX", TheoreticalQty: 14.5, UnitCost: 23.75, ETag: `W/"JzQ0O0VqYz0nOyc="`,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %+v\nwant    %+v", got, want)
	}
	if r := f.requests()[0]; r.Filter != "worksheetSeqNo eq 7" {
		t.Errorf("decoded $filter = %q, want %q (number unquoted)", r.Filter, "worksheetSeqNo eq 7")
	}
}

func TestRetailItemsAndSKUCostsAreNoOps(t *testing.T) {
	f := newFake(t)
	c := f.client()
	items, err := c.GetRetailItems(ctx, []string{"1001"})
	if items != nil || err != nil {
		t.Errorf("GetRetailItems = %v, %v; want nil, nil", items, err)
	}
	skus, err := c.GetSKUCosts(ctx, "L10", []string{"1001"})
	if skus != nil || err != nil {
		t.Errorf("GetSKUCosts = %v, %v; want nil, nil", skus, err)
	}
	if n := len(f.requests()) + f.tokenCount(); n != 0 {
		t.Errorf("no-op methods made %d requests", n)
	}
}

// ── PostFinalCounts ─────────────────────────────────────────────────────────

func TestPostFinalCounts(t *testing.T) {
	type patch struct{ Path, IfMatch, Body string }
	base := companyPath + "/stockCountLines"
	tests := []struct {
		name        string
		failID      string
		failMsg     string
		in          []ls.FinalCountLine
		wantPatches []patch
		wantErr     []string
	}{
		{
			name: "etags, star for a line without one, decimal quantities",
			in: []ls.FinalCountLine{
				{ItemNo: "1001", LineNo: 10000, CountedQty: 12},
				{ItemNo: "1002", LineNo: 20000, CountedQty: 0.5},
				{ItemNo: "1003", LineNo: 30000, CountedQty: 0},
			},
			wantPatches: []patch{
				{base + "(id-10000)", `W/"e10000"`, `{"countedQty":12}`},
				{base + "(id-20000)", "*", `{"countedQty":0.5}`},
				{base + "(id-30000)", `W/"e30000"`, `{"countedQty":0}`},
			},
		},
		{
			name: "line numbers not on the count are skipped",
			in: []ls.FinalCountLine{
				{ItemNo: "X", LineNo: 99999, CountedQty: 5},
				{ItemNo: "1003", LineNo: 30000, CountedQty: 7},
				{ItemNo: "Y", LineNo: 15000, CountedQty: 1},
			},
			wantPatches: []patch{{base + "(id-30000)", `W/"e30000"`, `{"countedQty":7}`}},
		},
		{
			name:    "first failure stops the run and names the line",
			failID:  "id-20000",
			failMsg: "Another user has modified the record for this Stock Count Line.",
			in: []ls.FinalCountLine{
				{ItemNo: "1001", LineNo: 10000, CountedQty: 1},
				{ItemNo: "1002", LineNo: 20000, CountedQty: 2},
				{ItemNo: "1003", LineNo: 30000, CountedQty: 3},
			},
			wantPatches: []patch{
				{base + "(id-10000)", `W/"e10000"`, `{"countedQty":1}`},
				{base + "(id-20000)", "*", `{"countedQty":2}`},
			},
			wantErr: []string{"line 20000", "412", "Another user has modified the record"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t)
			f.lines = []map[string]any{
				line("id-10000", 7, 10000, "1001", `W/"e10000"`),
				line("id-20000", 7, 20000, "1002", ""),
				line("id-30000", 7, 30000, "1003", `W/"e30000"`),
				line("id-other", 8, 15000, "1001", `W/"other"`), // same line no on another count
			}
			if tt.failID != "" {
				f.patchStatus[tt.failID] = http.StatusPreconditionFailed
				f.patchErrMsg = tt.failMsg
			}
			err := f.client().PostFinalCounts(ctx, 7, tt.in)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("PostFinalCounts: %v", err)
			}
			if tt.wantErr != nil {
				mustContain(t, err, tt.wantErr...)
			}
			var got []patch
			for _, r := range f.requestsByMethod(http.MethodPatch) {
				if r.ContentType != "application/json" {
					t.Errorf("PATCH %s Content-Type = %q", r.Path, r.ContentType)
				}
				got = append(got, patch{r.Path, r.IfMatch, r.Body})
			}
			if !reflect.DeepEqual(got, tt.wantPatches) {
				t.Errorf("PATCHes =\n  %+v\nwant\n  %+v", got, tt.wantPatches)
			}
			if gets := f.requestsByMethod(http.MethodGet); len(gets) != 1 || gets[0].Filter != "worksheetSeqNo eq 7" {
				t.Errorf("line lookup = %+v, want one GET filtered on worksheetSeqNo eq 7", gets)
			}
		})
	}
}

// ── ClearWorksheetLines ─────────────────────────────────────────────────────

func TestClearWorksheetLines(t *testing.T) {
	tests := []struct {
		name       string
		seq        int
		status     int
		msg        string
		wantAction string
		wantErr    []string
	}{
		{name: "204", seq: 7, status: http.StatusNoContent, wantAction: companyPath + "/stockCounts(guid-7)/Microsoft.NAV.resetCounts"},
		{name: "200", seq: 7, status: http.StatusOK, wantAction: companyPath + "/stockCounts(guid-7)/Microsoft.NAV.resetCounts"},
		{name: "no count with that seq", seq: 42, wantErr: []string{"worksheetSeqNo 42"}},
		{name: "BC refuses", seq: 7, status: http.StatusBadRequest, msg: "Stock count SC000007 is posted and cannot be reset.",
			wantAction: companyPath + "/stockCounts(guid-7)/Microsoft.NAV.resetCounts",
			wantErr:    []string{"400", "Stock count SC000007 is posted and cannot be reset."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t)
			f.counts = []map[string]any{
				{"id": "guid-6", "number": "SC000006", "worksheetSeqNo": 6, "status": "Counting"},
				{"id": "guid-7", "number": "SC000007", "worksheetSeqNo": 7, "status": "Counting"},
			}
			f.lines = []map[string]any{line("l1", 7, 10000, "1001", "")}
			f.actionStatus, f.actionErrMsg = tt.status, tt.msg
			err := f.client().ClearWorksheetLines(ctx, tt.seq)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("ClearWorksheetLines: %v", err)
			}
			if tt.wantErr != nil {
				mustContain(t, err, tt.wantErr...)
			}
			gets := f.requestsByMethod(http.MethodGet)
			if len(gets) != 1 || !strings.HasSuffix(gets[0].Path, "/stockCounts") || gets[0].Filter != fmt.Sprintf("worksheetSeqNo eq %d", tt.seq) {
				t.Errorf("lookup = %+v, want one GET stockCounts?$filter=worksheetSeqNo eq %d", gets, tt.seq)
			}
			posts := f.requestsByMethod(http.MethodPost)
			if tt.wantAction == "" {
				if len(posts) != 0 {
					t.Errorf("POSTs = %+v, want none", posts)
				}
			} else {
				if len(posts) != 1 {
					t.Fatalf("POSTs = %d, want 1", len(posts))
				}
				p := posts[0]
				if p.Path != tt.wantAction || p.Body != "{}" || p.ContentType != "application/json" {
					t.Errorf("POST = path %q body %q content-type %q", p.Path, p.Body, p.ContentType)
				}
			}
			if d := f.requestsByMethod(http.MethodDelete); len(d) != 0 {
				t.Errorf("Vantage must reset counts, not delete lines; got %d DELETEs", len(d))
			}
		})
	}
}

// ── Errors and literals ─────────────────────────────────────────────────────

func TestErrorsSurfaceBCMessage(t *testing.T) {
	f := newFake(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, bcErr("BadRequest_NotFound", "The entity set 'stockCountLines' was not found."))
	}))
	defer srv.Close()
	c := f.client()
	c.apiBase = srv.URL
	_, err := c.GetWorksheetLines(ctx, 7)
	mustContain(t, err, "400", "BadRequest_NotFound", "The entity set 'stockCountLines' was not found.")
}

func TestHTTPError(t *testing.T) {
	long := strings.Repeat("x", 2000)
	tests := []struct {
		name    string
		status  int
		body    string
		want    []string
		maxBody int
	}{
		{"bc error", 409, `{"error":{"code":"Internal_X","message":"Boom"}}`, []string{"409", "Internal_X: Boom"}, 0},
		{"entra error", 400, `{"error":"invalid_scope","error_description":"AADSTS70011 bad scope"}`, []string{"400", "AADSTS70011 bad scope"}, 0},
		{"not json", 502, "<html>Bad gateway</html>", []string{"502", "<html>Bad gateway</html>"}, 0},
		{"body capped at 512 bytes", 500, long, []string{"500"}, 512},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := httpError(tt.status, []byte(tt.body))
			mustContain(t, err, tt.want...)
			if tt.maxBody > 0 {
				if n := strings.Count(err.Error(), "x"); n != tt.maxBody {
					t.Errorf("body bytes in error = %d, want %d", n, tt.maxBody)
				}
			}
		})
	}
}

func TestODataString(t *testing.T) {
	tests := map[string]string{
		"Counting":  "'Counting'",
		"O'Brien's": "'O''Brien''s'",
		"":          "''",
		"a''b":      "'a''''b'",
	}
	for in, want := range tests {
		if got := odataString(in); got != want {
			t.Errorf("odataString(%q) = %q, want %q", in, got, want)
		}
	}
}
