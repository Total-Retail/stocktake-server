package auth

import (
	"context"

	"gorm.io/gorm"
)

// 11 Oct 2026 (Wyne) — only counters an admin put on a stock count may log in, and a
// counter may open only the sessions it is on. Before, any phone number that received
// an OTP became a counter, and a counter could read and post to any session.

// CounterMayLogIn reports whether the mobile number belongs to a counter that is active
// on at least one session.
func CounterMayLogIn(ctx context.Context, db *gorm.DB, mobile string) (bool, error) {
	var n int64
	err := db.WithContext(ctx).Raw(`
		SELECT COUNT(*) FROM counters c
		JOIN session_counters sc ON sc.counter_id = c.id AND sc.active
		WHERE c.mobile_number = ?`, mobile).Scan(&n).Error
	return n > 0, err
}

// CounterAssigned reports whether the counter is active on the session.
func CounterAssigned(ctx context.Context, db *gorm.DB, sessionID, counterID string) (bool, error) {
	var n int64
	err := db.WithContext(ctx).Raw(`
		SELECT COUNT(*) FROM session_counters
		WHERE session_id = ? AND counter_id = ? AND active`, sessionID, counterID).Scan(&n).Error
	return n > 0, err
}
