package reporting

import "time"

type CounterPerformance struct {
	CounterID       string    `json:"counter_id"`
	CounterName     string    `json:"counter_name"`
	Mobile          string    `json:"mobile"`
	ItemsCounted    int       `json:"items_counted"`
	BinsCompleted   int       `json:"bins_completed"`
	RecountRate     float64   `json:"recount_rate_pct"`
	RecountAccepted int       `json:"recount_accepted"`
	RecountRejected int       `json:"recount_rejected"`
	LastActivity    time.Time `json:"last_activity"`
}

type HourlyActivity struct {
	CounterID string `json:"counter_id"`
	Hour      int    `json:"hour"`
	Count     int    `json:"count"`
}

type SessionSummary struct {
	SessionID      string               `json:"session_id"`
	TotalItems     int                  `json:"total_items"`
	TotalBins      int                  `json:"total_bins"`
	BinsCompleted  int                  `json:"bins_completed"`
	TotalCounts    int                  `json:"total_counts"`
	Counters       []CounterPerformance `json:"counters"`
	HourlyActivity []HourlyActivity     `json:"hourly_activity"`
}
