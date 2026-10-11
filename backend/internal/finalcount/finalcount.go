// Package finalcount holds the one rule for an item's final counted quantity in a
// session, so what is posted to the ERP, what the variance screen shows and what
// the Excel export prints are always the same figure.
//
// The rule: per item, bin and counter only the latest count line counts (the mobile
// keeps one row per item, bin and round and updates it; a second row for the same
// slot — a second device, a reinstalled app — replaces the first rather than adding
// to it); per item only the highest recount round counts; what is left is summed
// across bins and counters.
package finalcount

import (
	"context"

	"gorm.io/gorm"
)

// CTE defines final_counts(item_no, total_qty) for one session. It takes one
// positional parameter, the session id. Use it as the start of a query:
//
//	db.Raw(finalcount.CTE+` SELECT ... LEFT JOIN final_counts fc ON fc.item_no = ...`, sessionID, ...)
const CTE = `WITH latest_per_slot AS (
		SELECT DISTINCT ON (item_no, bin_id, counter_id)
			item_no, bin_id, counter_id, quantity, round_no
		FROM count_lines
		WHERE session_id = ?
		ORDER BY item_no, bin_id, counter_id, counted_at DESC, synced_at DESC
	),
	max_rounds AS (
		SELECT item_no, MAX(round_no) AS max_round
		FROM latest_per_slot
		GROUP BY item_no
	),
	final_counts AS (
		SELECT l.item_no, COALESCE(SUM(l.quantity), 0) AS total_qty
		FROM latest_per_slot l
		JOIN max_rounds m ON m.item_no = l.item_no AND l.round_no = m.max_round
		GROUP BY l.item_no
	)`

// Totals returns each counted item's final quantity in the session.
func Totals(ctx context.Context, db *gorm.DB, sessionID string) (map[string]float64, error) {
	var rows []struct {
		ItemNo   string
		TotalQty float64
	}
	if err := db.WithContext(ctx).Raw(CTE+` SELECT item_no, total_qty FROM final_counts`, sessionID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(rows))
	for _, r := range rows {
		out[r.ItemNo] = r.TotalQty
	}
	return out, nil
}
