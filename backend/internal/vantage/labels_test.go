package vantage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// labelFake is a minimal Entra + BC pair for the label calls: handler answers every API
// request; the requests are recorded.
type labelFake struct {
	srv     *httptest.Server
	handler func(w http.ResponseWriter, r *http.Request, body string)
	reqs    []recordedReq
}

func newLabelFake(t *testing.T, h func(w http.ResponseWriter, r *http.Request, body string)) (*labelFake, *Client) {
	f := &labelFake{handler: h}
	mux := http.NewServeMux()
	mux.HandleFunc("/tnt/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "tok", "expires_in": 3599})
	})
	mux.HandleFunc("/v2.0/", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.reqs = append(f.reqs, recordedReq{
			Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery, Filter: r.URL.Query().Get("$filter"),
			IfMatch: r.Header.Get("If-Match"), ContentType: r.Header.Get("Content-Type"), Body: string(b),
		})
		f.handler(w, r, string(b))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	c := NewClient(Config{
		TenantID: "tnt", Environment: "env", CompanyID: "c0ffee00-0000-0000-0000-000000000001",
		ClientID: "id", ClientSecret: "secret",
		TokenURL:   f.srv.URL + "/tnt/oauth2/v2.0/token",
		APIBaseURL: f.srv.URL + companyPath,
	})
	return f, c
}

const wsID = "11111111-2222-3333-4444-555555555555"
const lineID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func TestAddLabelScansDecodesWrappedValue(t *testing.T) {
	inner := `{"worksheetNo":"LW00001","results":[
	  {"clientScanId":"6f9619ff-8b86-d011-b42d-00c04fc964ff","barcode":"6001","result":"added","originalResult":null,
	   "worksheetNo":"LW00001","processedAt":"2026-11-24T08:00:03.120Z",
	   "line":{"id":"` + lineID + `","worksheetNo":"LW00001","lineNo":10000,"itemNo":"1896-S","description":"ATHENS Desk",
	           "unitPrice":649.4,"currencyCode":"USD","priceDate":"2026-11-24","quantity":2,"printedQty":0,
	           "lastScannedAt":"2026-11-24T08:00:00.000Z","promotionEndingDate":null},"error":null},
	  {"clientScanId":"7f9619ff-8b86-d011-b42d-00c04fc964ff","barcode":"NOPE","result":"error","originalResult":null,
	   "line":null,"error":"NOPE is not a barcode of any item."}]}`
	f, c := newLabelFake(t, func(w http.ResponseWriter, r *http.Request, body string) {
		writeJSON(w, http.StatusOK, map[string]any{"@odata.context": "x/$metadata#Edm.String", "value": inner})
	})
	res, err := c.AddLabelScans(context.Background(), wsID, []LabelScan{
		{ClientScanID: "6f9619ff-8b86-d011-b42d-00c04fc964ff", Barcode: "6001", Quantity: 2, ScannedBy: "Mascline", ScannedAt: "2026-11-24T08:00:00Z"},
		{ClientScanID: "7f9619ff-8b86-d011-b42d-00c04fc964ff", Barcode: "NOPE", Quantity: 1, ScannedBy: "Mascline"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Result != "added" || res[0].Line == nil || res[0].Line.Quantity != 2 || res[0].Line.ItemNo != "1896-S" {
		t.Fatalf("first result wrong: %+v", res[0])
	}
	if res[1].Result != "error" || res[1].Error == nil || !strings.Contains(*res[1].Error, "NOPE") || res[1].Line != nil {
		t.Fatalf("second result wrong: %+v", res[1])
	}

	r := f.reqs[0]
	if r.Method != http.MethodPost || r.Path != companyPath+"/labelWorksheets("+wsID+")/Microsoft.NAV.addScans" {
		t.Fatalf("request %s %s", r.Method, r.Path)
	}
	// The body is {"scansJson": "<json array text>"}.
	var params map[string]string
	if err := json.Unmarshal([]byte(r.Body), &params); err != nil {
		t.Fatalf("body not an object of strings: %s", r.Body)
	}
	var sent []map[string]any
	if err := json.Unmarshal([]byte(params["scansJson"]), &sent); err != nil || len(sent) != 2 {
		t.Fatalf("scansJson %q: %v", params["scansJson"], err)
	}
	if sent[0]["clientScanId"] != "6f9619ff-8b86-d011-b42d-00c04fc964ff" || sent[0]["quantity"] != float64(2) ||
		sent[0]["scannedBy"] != "Mascline" || sent[0]["scannedAt"] != "2026-11-24T08:00:00Z" {
		t.Fatalf("scan 0 sent as %v", sent[0])
	}
	if _, ok := sent[1]["scannedAt"]; ok {
		t.Fatalf("empty scannedAt should be left out: %v", sent[1])
	}
}

func TestAddLabelScansAcceptsUnwrappedBody(t *testing.T) {
	_, c := newLabelFake(t, func(w http.ResponseWriter, r *http.Request, body string) {
		io.WriteString(w, `{"worksheetNo":"LW00001","results":[{"clientScanId":"x","barcode":"6001","result":"incremented","line":null,"error":null}]}`)
	})
	res, err := c.AddLabelScans(context.Background(), wsID, []LabelScan{{ClientScanID: "x", Barcode: "6001", Quantity: 1}})
	if err != nil || len(res) != 1 || res[0].Result != "incremented" {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestAddLabelScansCountMismatch(t *testing.T) {
	_, c := newLabelFake(t, func(w http.ResponseWriter, r *http.Request, body string) {
		writeJSON(w, http.StatusOK, map[string]any{"value": `{"results":[]}`})
	})
	_, err := c.AddLabelScans(context.Background(), wsID, []LabelScan{{ClientScanID: "x", Barcode: "6001", Quantity: 1}})
	mustContain(t, err, "sent 1 scans", "answered 0")
}

func TestLabelRefusalIsAPIErrorWithoutCorrelationID(t *testing.T) {
	_, c := newLabelFake(t, func(w http.ResponseWriter, r *http.Request, body string) {
		writeJSON(w, http.StatusBadRequest, bcErr("Application_DialogException",
			"Label worksheet LW00001 has been printed. Reopen it to add or change lines, or to print it again.  CorrelationId: 0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0."))
	})
	_, err := c.AddLabelScans(context.Background(), wsID, []LabelScan{{ClientScanID: "x", Barcode: "6001", Quantity: 1}})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T %v", err, err)
	}
	if apiErr.Status != 400 || apiErr.Message != "Label worksheet LW00001 has been printed. Reopen it to add or change lines, or to print it again." {
		t.Fatalf("message %q", apiErr.Message)
	}
}

func TestListLabelWorksheetsFilter(t *testing.T) {
	f, c := newLabelFake(t, func(w http.ResponseWriter, r *http.Request, body string) {
		writeJSON(w, http.StatusOK, map[string]any{"value": []map[string]any{
			{"id": wsID, "number": "LW00001", "storeNo": "5002", "status": "Open", "ownerId": "o'b", "noOfLines": 3, "totalLabels": 5},
		}})
	})
	got, err := c.ListLabelWorksheets(context.Background(), LabelWorksheetFilter{StoreNo: "5002", OwnerID: "o'b", Status: "Open"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Number != "LW00001" || got[0].TotalLabels != 5 {
		t.Fatalf("got %+v", got)
	}
	if want := "storeNo eq '5002' and ownerId eq 'o''b' and status eq 'Open'"; f.reqs[0].Filter != want {
		t.Fatalf("filter %q, want %q", f.reqs[0].Filter, want)
	}
}

func TestGetLabelWorksheetExpandsLines(t *testing.T) {
	f, c := newLabelFake(t, func(w http.ResponseWriter, r *http.Request, body string) {
		writeJSON(w, http.StatusOK, map[string]any{
			"@odata.etag": `W/"JzE5OzEn"`, "id": wsID, "number": "LW00001", "status": "Open",
			"labelWorksheetLines": []map[string]any{
				{"@odata.etag": `W/"l1"`, "id": lineID, "lineNo": 10000, "itemNo": "1896-S", "quantity": 2,
					"priceDate": "0001-01-01", "lastScannedAt": "0001-01-01T00:00:00Z"},
			},
		})
	})
	ws, err := c.GetLabelWorksheet(context.Background(), wsID, true)
	if err != nil {
		t.Fatal(err)
	}
	if ws.ETag != `W/"JzE5OzEn"` || len(ws.Lines) != 1 || ws.Lines[0].ETag != `W/"l1"` || ws.Lines[0].Quantity != 2 {
		t.Fatalf("got %+v", ws)
	}
	if f.reqs[0].RawQuery != "$expand=labelWorksheetLines" {
		t.Fatalf("query %q", f.reqs[0].RawQuery)
	}
}

func TestLabelNotFound(t *testing.T) {
	_, c := newLabelFake(t, func(w http.ResponseWriter, r *http.Request, body string) {
		writeJSON(w, http.StatusNotFound, bcErr("BadRequest_ResourceNotFound", "Resource not found"))
	})
	if _, err := c.GetLabelWorksheet(context.Background(), wsID, true); !errors.Is(err, ErrLabelNotFound) {
		t.Fatalf("want ErrLabelNotFound, got %v", err)
	}
}

func TestLabelIDsMustBeGUIDs(t *testing.T) {
	f, c := newLabelFake(t, func(w http.ResponseWriter, r *http.Request, body string) {
		t.Fatalf("no request expected, got %s %s", r.Method, r.URL.Path)
	})
	for _, id := range []string{"", "x", wsID + ")/other(", "../labelFormats"} {
		if _, err := c.GetLabelWorksheet(context.Background(), id, false); !errors.Is(err, ErrLabelNotFound) {
			t.Fatalf("%q: want ErrLabelNotFound, got %v", id, err)
		}
		if err := c.DeleteLabelWorksheetLine(context.Background(), id, "*"); !errors.Is(err, ErrLabelNotFound) {
			t.Fatalf("%q: want ErrLabelNotFound, got %v", id, err)
		}
	}
	if len(f.reqs) != 0 {
		t.Fatalf("requests sent: %v", f.reqs)
	}
}

func TestLineQtyPatchAndDeleteUseIfMatch(t *testing.T) {
	f, c := newLabelFake(t, func(w http.ResponseWriter, r *http.Request, body string) {
		if r.Method == http.MethodPatch {
			writeJSON(w, http.StatusOK, map[string]any{"id": lineID, "quantity": 5})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.UpdateLabelWorksheetLineQty(context.Background(), lineID, `W/"l1"`, 5); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteLabelWorksheetLine(context.Background(), lineID, ""); err != nil {
		t.Fatal(err)
	}
	p, d := f.reqs[0], f.reqs[1]
	if p.Method != http.MethodPatch || p.Path != companyPath+"/labelWorksheetLines("+lineID+")" ||
		p.IfMatch != `W/"l1"` || p.Body != `{"quantity":5}` {
		t.Fatalf("patch %+v", p)
	}
	if d.Method != http.MethodDelete || d.IfMatch != "*" {
		t.Fatalf("delete %+v", d)
	}
}

func TestCreateLabelWorksheet(t *testing.T) {
	f, c := newLabelFake(t, func(w http.ResponseWriter, r *http.Request, body string) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": wsID, "number": "LW00002", "storeNo": "5002", "status": "Open"})
	})
	ws, err := c.CreateLabelWorksheet(context.Background(), NewLabelWorksheet{
		StoreNo: "5002", Description: "Aisle 4", OwnerID: "c1", OwnerName: "Mascline", DeviceID: "TC21-07",
	})
	if err != nil || ws.Number != "LW00002" || ws.Lines == nil {
		t.Fatalf("ws %+v err %v", ws, err)
	}
	want := `{"storeNo":"5002","description":"Aisle 4","ownerId":"c1","ownerName":"Mascline","deviceId":"TC21-07"}`
	if f.reqs[0].Body != want {
		t.Fatalf("body %s", f.reqs[0].Body)
	}
}
