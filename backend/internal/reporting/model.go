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
	// How the counter was identified on their lines: "OTP", "NFC" or "NFC, OTP".
	AuthMethods string `json:"auth_methods"`
	// DevicesUsed is how many shared devices the counter counted on (0 = own sign-in only).
	DevicesUsed int `json:"devices_used"`
	// CountLines is how many count lines the counter sent (every scan/edit slot).
	CountLines int `json:"count_lines"`
}

// DeviceActivity is one device's share of a session: the shared device by name, or
// DeviceID nil for counters signed in on their own (phone + OTP).
type DeviceActivity struct {
	DeviceID     *string    `json:"device_id"`
	DeviceName   string     `json:"device_name"`
	AuthMethod   string     `json:"auth_method"`
	CountLines   int        `json:"count_lines"`
	ItemsCounted int        `json:"items_counted"`
	BinsCounted  int        `json:"bins_counted"`
	CounterCount int        `json:"counter_count"`
	Counters     string     `json:"counters"`
	FirstCount   *time.Time `json:"first_count"`
	LastCount    *time.Time `json:"last_count"`
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
