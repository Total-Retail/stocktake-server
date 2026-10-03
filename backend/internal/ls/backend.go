package ls

import "context"

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
}

var _ Backend = (*Client)(nil)
