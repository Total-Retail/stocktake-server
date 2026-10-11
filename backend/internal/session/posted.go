package session

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/totalretail/stocktake/internal/ls"
	"github.com/totalretail/stocktake/internal/ws"
)

// C4/H2 (11 Oct 2026) — a submitted session becomes POSTED, with the ERP's document
// number, once finance posts the count's journal. The ERP is asked on a timer
// (StartDocumentPoller); a backend that cannot tell (LS Central for now) leaves it to
// an admin, who enters the number by hand (SetDocumentNumber).

// CheckPostedDocuments asks the ERP about every session waiting in PENDING_REVIEW and
// marks the posted ones POSTED. Returns how many it marked. A backend that cannot tell
// returns ls.ErrPostedDocumentUnsupported straight away.
func (s *service) CheckPostedDocuments(ctx context.Context) (int, error) {
	var waiting []Session
	if err := s.db.WithContext(ctx).
		Where("status = ? AND worksheet_no IS NOT NULL AND worksheet_no <> ''", StatusPendingReview).
		Find(&waiting).Error; err != nil {
		return 0, err
	}
	marked := 0
	for i := range waiting {
		sess := &waiting[i]
		seqNo := worksheetSeqNoFromSession(sess)
		if seqNo == 0 {
			continue
		}
		docNo, err := s.lsClient.PostedDocumentNo(ctx, seqNo)
		if errors.Is(err, ls.ErrPostedDocumentUnsupported) {
			return marked, err
		}
		now := time.Now()
		if err != nil {
			log.Printf("WARN [documents] session=%s worksheet=%d: %v", sess.ID, seqNo, err)
			continue
		}
		if docNo == "" {
			s.db.WithContext(ctx).Model(&Session{}).Where("id = ?", sess.ID).Update("document_checked_at", now)
			continue
		}
		if err := s.markPosted(ctx, sess.ID, docNo, now); err != nil {
			log.Printf("WARN [documents] session=%s: %v", sess.ID, err)
			continue
		}
		marked++
	}
	return marked, nil
}

// SetDocumentNumber marks a submitted session POSTED with the document number an
// admin read from the ERP.
func (s *service) SetDocumentNumber(ctx context.Context, id, documentNo string) error {
	documentNo = strings.TrimSpace(documentNo)
	if documentNo == "" {
		return fmt.Errorf("document number is required")
	}
	sess, err := s.GetSession(ctx, id)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}
	if sess.Status != StatusPendingReview {
		return fmt.Errorf("only a submitted session (PENDING_REVIEW) can be marked posted; this one is %s", sess.Status)
	}
	return s.markPosted(ctx, id, documentNo, time.Now())
}

func (s *service) markPosted(ctx context.Context, id, documentNo string, at time.Time) error {
	res := s.db.WithContext(ctx).Model(&Session{}).
		Where("id = ? AND status = ?", id, StatusPendingReview).
		Updates(map[string]interface{}{
			"status":              StatusPosted,
			"document_number":     documentNo,
			"document_checked_at": at,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("session %s is no longer waiting for its document", id)
	}
	s.hub.Broadcast(id, ws.Event{
		Type:      ws.EventSessionUpdated,
		SessionID: id,
		Payload:   map[string]string{"status": string(StatusPosted), "document_number": documentNo},
	})
	return nil
}

// StartDocumentPoller runs CheckPostedDocuments every interval until ctx ends. It stops
// for good, with one log line, when the backend cannot report posted documents.
// interval <= 0 disables it.
func StartDocumentPoller(ctx context.Context, svc Service, interval time.Duration) {
	if interval <= 0 {
		log.Printf("INFO [documents] posted-document check disabled (DOCUMENT_CHECK_INTERVAL_MINUTES=0)")
		return
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			n, err := svc.CheckPostedDocuments(ctx)
			if errors.Is(err, ls.ErrPostedDocumentUnsupported) {
				log.Printf("INFO [documents] %v; mark sessions POSTED by hand", err)
				return
			}
			if err != nil {
				log.Printf("WARN [documents] check failed: %v", err)
			} else if n > 0 {
				log.Printf("INFO [documents] %d session(s) marked POSTED", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
