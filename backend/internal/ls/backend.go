package ls

import (
	"context"
	"errors"
)

// Backend is the ERP surface the session service needs. *Client implements it
// against LS Central (on-prem OData, basic auth); vantage.Client implements it
// against the Vantage Retail API on Business Central online. One deployment
// uses one backend, chosen by ERP_BACKEND.
type Backend interface {
	GetLSStores(ctx context.Context) ([]LSStore, error)
	GetAvailableWorksheets(ctx context.Context) ([]AvailableWorksheet, error)
	GetWorksheetLines(ctx context.Context, worksheetSeqNo int) ([]WorksheetLine, error)
	GetRetailItems(ctx context.Context, itemNos []string) ([]RetailItemLine, error)
	GetSKUCosts(ctx context.Context, locationCode string, itemNos []string) ([]SKULine, error)
	PostFinalCounts(ctx context.Context, worksheetSeqNo int, lines []FinalCountLine) error
	ClearWorksheetLines(ctx context.Context, worksheetSeqNo int) error
	// PostedDocumentNo returns the ERP document number once the count's journal is
	// posted, "" while it is not. ErrPostedDocumentUnsupported when the backend cannot
	// tell; the session is then marked POSTED by hand (POST /sessions/:id/document-number).
	PostedDocumentNo(ctx context.Context, worksheetSeqNo int) (string, error)
}

// ErrPostedDocumentUnsupported is returned by a backend that cannot see whether a
// worksheet's journal has been posted.
var ErrPostedDocumentUnsupported = errors.New("this ERP backend cannot report the posted document number")

var _ Backend = (*Client)(nil)
