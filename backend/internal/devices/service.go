package devices

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Errors the handlers turn into status codes.
var (
	ErrNotFound      = errors.New("not found")
	ErrBadInput      = errors.New("a name and a mobile number are needed")
	ErrDeviceRevoked = errors.New("this device is no longer set up for shared counting; ask a supervisor to set it up again")
	ErrTagUnknown    = errors.New("this card is not linked to a counter; ask a supervisor to link it")
	ErrNotOnCount    = errors.New("you are not on a stock count on this device's store, or set up for label printing; ask your supervisor to add you")
)

// TagInUseError is returned when a card is linked to someone else and the caller did
// not ask to move it.
type TagInUseError struct {
	CounterID   string `json:"counter_id"`
	CounterName string `json:"counter_name"`
}

func (e *TagInUseError) Error() string {
	return fmt.Sprintf("this card is already linked to %s", e.CounterName)
}

// openStatuses are the session statuses a counter can count on: the same ones the
// counter's session list shows (session.GetCounterSessions), so a counter let in by
// their card always finds their count on the device.
var openStatuses = []string{"ACTIVE", "REOPENED"}

// EnsureIndexes creates what AutoMigrate cannot: one active card per counter and one
// active counter per card.
func EnsureIndexes(db *gorm.DB) error {
	for _, sql := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS udx_counter_tags_uid_active ON counter_tags(uid) WHERE active`,
		`CREATE UNIQUE INDEX IF NOT EXISTS udx_counter_tags_counter_active ON counter_tags(counter_id) WHERE active`,
		`CREATE INDEX IF NOT EXISTS idx_count_lines_device ON count_lines(session_id, device_id)`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			return fmt.Errorf("devices index: %w", err)
		}
	}
	return nil
}

func isUUID(s string) bool { _, err := uuid.Parse(s); return err == nil }

// ─── Devices ─────────────────────────────────────────────────────────────────

// EnrolRequest is what a supervisor sends from the device being enrolled.
type EnrolRequest struct {
	Name       string  `json:"name" binding:"required"`
	InstallID  string  `json:"install_id" binding:"required"`
	StoreID    *string `json:"store_id"`
	Model      string  `json:"model"`
	AppVersion string  `json:"app_version"`
}

// Enrol creates the device, or re-activates the same install, and bumps its token
// version so any token issued before stops working.
func Enrol(ctx context.Context, db *gorm.DB, req EnrolRequest, adminID string) (*Device, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.InstallID = strings.TrimSpace(req.InstallID)
	if req.StoreID != nil && (*req.StoreID == "" || !isUUID(*req.StoreID)) {
		req.StoreID = nil
	}
	var d Device
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Where("install_id = ?", req.InstallID).First(&d).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			d = Device{
				Name: req.Name, InstallID: req.InstallID, StoreID: req.StoreID,
				Model: req.Model, AppVersion: req.AppVersion, Active: true,
				TokenVersion: 1, EnrolledBy: adminID, EnrolledAt: time.Now(),
			}
			return tx.Create(&d).Error
		case err != nil:
			return err
		}
		return tx.Model(&d).Updates(map[string]interface{}{
			"name":          req.Name,
			"store_id":      req.StoreID,
			"model":         req.Model,
			"app_version":   req.AppVersion,
			"active":        true,
			"token_version": gorm.Expr("token_version + 1"),
			"enrolled_by":   adminID,
			"enrolled_at":   time.Now(),
			"revoked_at":    nil,
			"revoked_by":    nil,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return GetDevice(ctx, db, d.ID)
}

func GetDevice(ctx context.Context, db *gorm.DB, id string) (*Device, error) {
	if !isUUID(id) {
		return nil, ErrNotFound
	}
	var d Device
	if err := db.WithContext(ctx).First(&d, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}

// ListDevices returns every enrolled device, newest first, with store and supervisor names.
func ListDevices(ctx context.Context, db *gorm.DB) ([]DeviceView, error) {
	var out []DeviceView
	err := db.WithContext(ctx).Raw(`
		SELECT d.*, s.store_name AS store_name, a.full_name AS enrolled_by_name
		FROM devices d
		LEFT JOIN stores s ON s.id = d.store_id
		LEFT JOIN admin_users a ON a.id = d.enrolled_by
		ORDER BY d.active DESC, d.enrolled_at DESC`).Scan(&out).Error
	if out == nil {
		out = []DeviceView{}
	}
	return out, err
}

// UpdateDevice renames the device and, when setStore, moves it to another store (nil:
// any store).
func UpdateDevice(ctx context.Context, db *gorm.DB, id, name string, setStore bool, storeID *string) (*Device, error) {
	if _, err := GetDevice(ctx, db, id); err != nil {
		return nil, err
	}
	if storeID != nil && (*storeID == "" || !isUUID(*storeID)) {
		storeID = nil
	}
	updates := map[string]interface{}{}
	if setStore {
		updates["store_id"] = storeID
	}
	if n := strings.TrimSpace(name); n != "" {
		updates["name"] = n
	}
	if len(updates) > 0 {
		if err := db.WithContext(ctx).Model(&Device{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return GetDevice(ctx, db, id)
}

// RevokeDevice stops the device's token working at once. Counts already synced stay.
func RevokeDevice(ctx context.Context, db *gorm.DB, id, adminID string) error {
	if _, err := GetDevice(ctx, db, id); err != nil {
		return err
	}
	return db.WithContext(ctx).Model(&Device{}).Where("id = ?", id).Updates(map[string]interface{}{
		"active":        false,
		"revoked_at":    time.Now(),
		"revoked_by":    adminID,
		"token_version": gorm.Expr("token_version + 1"),
	}).Error
}

// CheckDeviceToken is the per-request check behind a device token: the device must still
// be active and the token must be the latest issued to it.
func CheckDeviceToken(ctx context.Context, db *gorm.DB, deviceID string, tokenVersion int) (*Device, error) {
	d, err := GetDevice(ctx, db, deviceID)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrDeviceRevoked
	}
	if err != nil {
		return nil, err
	}
	if !d.Active || d.TokenVersion != tokenVersion {
		return nil, ErrDeviceRevoked
	}
	return d, nil
}

// Touch records that the device was seen; at most once a minute, to spare the table.
func Touch(ctx context.Context, db *gorm.DB, deviceID string) {
	db.WithContext(ctx).Exec(`UPDATE devices SET last_seen_at = NOW()
		WHERE id = ? AND (last_seen_at IS NULL OR last_seen_at < NOW() - INTERVAL '1 minute')`, deviceID)
}

// SessionInDeviceStore reports whether the session may be counted on the device: always
// for a device with no store, else only the store's own sessions.
func SessionInDeviceStore(ctx context.Context, db *gorm.DB, d *Device, sessionID string) (bool, error) {
	if d.StoreID == nil {
		return true, nil
	}
	if !isUUID(sessionID) {
		return false, nil
	}
	var n int64
	err := db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM stock_count_sessions WHERE id = ? AND store_id = ?`,
		sessionID, *d.StoreID).Scan(&n).Error
	return n > 0, err
}

// CounterExists reports whether the id is a counter's.
func CounterExists(ctx context.Context, db *gorm.DB, counterID string) (bool, error) {
	if !isUUID(counterID) {
		return false, nil
	}
	var n int64
	err := db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM counters WHERE id = ?`, counterID).Scan(&n).Error
	return n > 0, err
}

// ─── Cards ───────────────────────────────────────────────────────────────────

// ListCounters returns counters with their active card. With sessionID, only the
// counters active on that session; with q, only names or numbers containing it.
func ListCounters(ctx context.Context, db *gorm.DB, sessionID, q string) ([]CounterWithTag, error) {
	sql := `
		SELECT c.id, c.name, c.mobile_number, c.can_print_labels, c.created_at,
		       t.uid AS tag_uid, t.label AS tag_label,
		       t.assigned_at AS tag_assigned_at, t.last_used_at AS tag_last_used_at
		FROM counters c
		LEFT JOIN counter_tags t ON t.counter_id = c.id AND t.active`
	var args []interface{}
	var where []string
	if sessionID != "" {
		if !isUUID(sessionID) {
			return []CounterWithTag{}, nil
		}
		sql += ` JOIN session_counters sc ON sc.counter_id = c.id AND sc.active AND sc.session_id = ?`
		args = append(args, sessionID)
	}
	if q = strings.TrimSpace(q); q != "" {
		where = append(where, `(c.name ILIKE ? OR c.mobile_number LIKE ?)`)
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	sql += ` ORDER BY c.name`
	var out []CounterWithTag
	err := db.WithContext(ctx).Raw(sql, args...).Scan(&out).Error
	if out == nil {
		out = []CounterWithTag{}
	}
	return out, err
}

// ActiveTagOwner returns the counter whose active card has this UID.
func ActiveTagOwner(ctx context.Context, db *gorm.DB, uid string) (*CounterWithTag, error) {
	var out []CounterWithTag
	if err := db.WithContext(ctx).Raw(`
		SELECT c.id, c.name, c.mobile_number, c.can_print_labels, c.created_at,
		       t.uid AS tag_uid, t.label AS tag_label,
		       t.assigned_at AS tag_assigned_at, t.last_used_at AS tag_last_used_at
		FROM counter_tags t JOIN counters c ON c.id = t.counter_id
		WHERE t.uid = ? AND t.active`, uid).Scan(&out).Error; err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrTagUnknown
	}
	return &out[0], nil
}

// LinkTag gives the counter this card. The counter's previous card is unlinked. A card
// linked to someone else is refused with *TagInUseError unless force (move it).
func LinkTag(ctx context.Context, db *gorm.DB, counterID, rawUID, label, adminID string, force bool) (*CounterWithTag, error) {
	uid, err := NormalizeUID(rawUID)
	if err != nil {
		return nil, err
	}
	ok, err := CounterExists(ctx, db, counterID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		owner, err := ActiveTagOwner(ctx, tx, uid)
		switch {
		case errors.Is(err, ErrTagUnknown):
		case err != nil:
			return err
		case owner.ID == counterID:
			// Same card, same counter: only the label may change.
			return tx.Model(&CounterTag{}).Where("uid = ? AND active", uid).
				Update("label", strings.TrimSpace(label)).Error
		case !force:
			return &TagInUseError{CounterID: owner.ID, CounterName: owner.Name}
		}
		now := time.Now()
		// Unlink the card from whoever had it, and the counter's own previous card.
		if err := tx.Model(&CounterTag{}).
			Where("active AND (uid = ? OR counter_id = ?)", uid, counterID).
			Updates(map[string]interface{}{"active": false, "revoked_at": now, "revoked_by": adminID}).Error; err != nil {
			return err
		}
		return tx.Create(&CounterTag{
			CounterID: counterID, UID: uid, Label: strings.TrimSpace(label),
			Active: true, AssignedBy: adminID, AssignedAt: now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return ActiveTagOwner(ctx, db, uid)
}

// UnlinkTag takes the counter's card away (lost card, counter left).
func UnlinkTag(ctx context.Context, db *gorm.DB, counterID, adminID string) error {
	res := db.WithContext(ctx).Model(&CounterTag{}).
		Where("counter_id = ? AND active", counterID).
		Updates(map[string]interface{}{"active": false, "revoked_at": time.Now(), "revoked_by": adminID})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Shared-device sign-in ───────────────────────────────────────────────────

// IdentifiedSession is a stock count the tapped-in counter is on.
type IdentifiedSession struct {
	ID             string `json:"id"`
	StoreName      string `json:"store_name"`
	StockCountDate string `json:"stock_count_date"`
	Type           string `json:"type"`
	Status         string `json:"status"`
}

// Identified is who tapped their card, and on which counts they may count.
type Identified struct {
	CounterID    string              `json:"counter_id"`
	Name         string              `json:"name"`
	MobileNumber string              `json:"mobile_number"`
	TagUID       string              `json:"tag_uid"`
	TagLabel     string              `json:"tag_label"`
	Sessions     []IdentifiedSession `json:"sessions"`
	// CanPrintLabels: the counter may use Label Printing (24 Nov 2026).
	CanPrintLabels bool `json:"can_print_labels"`
}

// sessionsFor returns the open sessions the counter is on, limited to the device's store
// when the device has one.
func sessionsFor(ctx context.Context, db *gorm.DB, counterID string, storeID *string) ([]IdentifiedSession, error) {
	sql := `
		SELECT s.id, st.store_name, s.session_date::text AS stock_count_date, s.type, s.status
		FROM session_counters sc
		JOIN stock_count_sessions s ON s.id = sc.session_id
		JOIN stores st ON st.id = s.store_id
		WHERE sc.counter_id = ? AND sc.active AND s.status IN ?`
	args := []interface{}{counterID, openStatuses}
	if storeID != nil {
		sql += ` AND s.store_id = ?`
		args = append(args, *storeID)
	}
	sql += ` ORDER BY s.session_date DESC`
	var out []IdentifiedSession
	err := db.WithContext(ctx).Raw(sql, args...).Scan(&out).Error
	if out == nil {
		out = []IdentifiedSession{}
	}
	return out, err
}

// Identify resolves a tapped card on a device to the counter, and records the tap.
func Identify(ctx context.Context, db *gorm.DB, d *Device, rawUID string) (*Identified, error) {
	uid, err := NormalizeUID(rawUID)
	if err != nil {
		return nil, err
	}
	owner, err := ActiveTagOwner(ctx, db, uid)
	if err != nil {
		return nil, err
	}
	sessions, err := sessionsFor(ctx, db, owner.ID, d.StoreID)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 && !owner.CanPrintLabels {
		return nil, ErrNotOnCount
	}
	db.WithContext(ctx).Model(&CounterTag{}).Where("uid = ? AND active", uid).Update("last_used_at", time.Now())
	return &Identified{
		CounterID: owner.ID, Name: owner.Name, MobileNumber: owner.MobileNumber,
		TagUID: uid, TagLabel: deref(owner.TagLabel), Sessions: sessions,
		CanPrintLabels: owner.CanPrintLabels,
	}, nil
}

// Roster is every carded counter the device may let in, so a tap works offline: those
// on an open count (of the device's store, when it has one) and, since 24 Nov 2026,
// those allowed to print labels (with no session, when they are on none).
func Roster(ctx context.Context, db *gorm.DB, d *Device) ([]RosterEntry, error) {
	storeCond := ""
	args := []interface{}{openStatuses}
	if d.StoreID != nil {
		storeCond = ` AND s.store_id = ?`
		args = append(args, *d.StoreID)
	}
	sql := `
		SELECT c.id AS counter_id, c.name, c.mobile_number, c.can_print_labels,
		       t.uid AS tag_uid, t.label AS tag_label, s.id AS session_id
		FROM counter_tags t
		JOIN counters c ON c.id = t.counter_id
		LEFT JOIN session_counters sc ON sc.counter_id = c.id AND sc.active
		LEFT JOIN stock_count_sessions s ON s.id = sc.session_id AND s.status IN ?` + storeCond + `
		WHERE t.active AND (s.id IS NOT NULL OR c.can_print_labels)
		ORDER BY c.name`
	var rows []struct {
		CounterID      string
		Name           string
		MobileNumber   string
		CanPrintLabels bool
		TagUID         string
		TagLabel       string
		SessionID      *string
	}
	if err := db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := []RosterEntry{}
	index := map[string]int{}
	for _, r := range rows {
		i, ok := index[r.CounterID]
		if !ok {
			index[r.CounterID] = len(out)
			out = append(out, RosterEntry{
				CounterID: r.CounterID, Name: r.Name, MobileNumber: r.MobileNumber,
				TagUID: r.TagUID, TagLabel: r.TagLabel, SessionIDs: []string{},
				CanPrintLabels: r.CanPrintLabels,
			})
			i = len(out) - 1
		}
		if r.SessionID != nil {
			out[i].SessionIDs = append(out[i].SessionIDs, *r.SessionID)
		}
	}
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ─── Staff (24 Nov 2026) ─────────────────────────────────────────────────────

// ErrMobileInUse is returned when another counter already has the mobile number.
var ErrMobileInUse = errors.New("another person already has this mobile number")

// CounterInput is what the portal's Staff page sends. Nil fields are left as they are.
type CounterInput struct {
	Name           *string `json:"name"`
	Mobile         *string `json:"mobile"`
	CanPrintLabels *bool   `json:"can_print_labels"`
}

// CreateCounter adds a person (name + mobile, normalized by the caller), or updates the
// one with that number: an admin adding someone already known edits them instead.
func CreateCounter(ctx context.Context, db *gorm.DB, name, mobile string, canPrintLabels bool) (*CounterWithTag, error) {
	name, mobile = strings.TrimSpace(name), strings.TrimSpace(mobile)
	if name == "" || mobile == "" {
		return nil, ErrBadInput
	}
	var id string
	err := db.WithContext(ctx).Raw(`
		INSERT INTO counters (id, name, mobile_number, can_print_labels, created_at)
		VALUES (gen_random_uuid(), ?, ?, ?, NOW())
		ON CONFLICT (mobile_number) DO UPDATE
		SET name = EXCLUDED.name, can_print_labels = EXCLUDED.can_print_labels
		RETURNING id`, name, mobile, canPrintLabels).Scan(&id).Error
	if err != nil {
		return nil, err
	}
	return getCounter(ctx, db, id)
}

// UpdateCounter changes a person's name, mobile number or label-printing permission.
func UpdateCounter(ctx context.Context, db *gorm.DB, id string, in CounterInput, normalizeMobile func(string) string) (*CounterWithTag, error) {
	ok, err := CounterExists(ctx, db, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	updates := map[string]interface{}{}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			return nil, ErrBadInput
		}
		updates["name"] = n
	}
	if in.Mobile != nil {
		m := normalizeMobile(*in.Mobile)
		if m == "" {
			return nil, ErrBadInput
		}
		var n int64
		if err := db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM counters WHERE mobile_number = ? AND id <> ?`, m, id).Scan(&n).Error; err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, ErrMobileInUse
		}
		updates["mobile_number"] = m
	}
	if in.CanPrintLabels != nil {
		updates["can_print_labels"] = *in.CanPrintLabels
	}
	if len(updates) > 0 {
		if err := db.WithContext(ctx).Table("counters").Where("id = ?", id).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return getCounter(ctx, db, id)
}

func getCounter(ctx context.Context, db *gorm.DB, id string) (*CounterWithTag, error) {
	var out []CounterWithTag
	if err := db.WithContext(ctx).Raw(`
		SELECT c.id, c.name, c.mobile_number, c.can_print_labels, c.created_at,
		       t.uid AS tag_uid, t.label AS tag_label,
		       t.assigned_at AS tag_assigned_at, t.last_used_at AS tag_last_used_at
		FROM counters c
		LEFT JOIN counter_tags t ON t.counter_id = c.id AND t.active
		WHERE c.id = ?`, id).Scan(&out).Error; err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return &out[0], nil
}
