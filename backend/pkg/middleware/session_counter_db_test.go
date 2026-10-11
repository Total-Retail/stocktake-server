package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/totalretail/stocktake/internal/auth"
	"github.com/totalretail/stocktake/internal/session"
	"github.com/totalretail/stocktake/internal/testdb"
	"github.com/totalretail/stocktake/pkg/middleware"
)

// 11 Oct 2026 (Wyne) — only counters an admin put on a stock count may log in, and only
// to the sessions they are on.
func TestCountersReachOnlyTheirOwnSessions(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	ada := auth.Counter{Name: "Ada", MobileNumber: "+263771000001"}
	ben := auth.Counter{Name: "Ben", MobileNumber: "+263771000002"}
	db.Create(&ada)
	db.Create(&ben)
	mine, other := uuid.NewString(), uuid.NewString()
	db.Create(&session.SessionCounter{SessionID: mine, CounterID: ada.ID, Active: true})
	db.Create(&session.SessionCounter{SessionID: other, CounterID: ben.ID, Active: true})
	db.Model(&session.SessionCounter{}).Where("counter_id = ?", ben.ID).Update("active", false)

	if ok, _ := auth.CounterMayLogIn(ctx, db, ada.MobileNumber); !ok {
		t.Error("Ada is on a session but may not log in")
	}
	if ok, _ := auth.CounterMayLogIn(ctx, db, ben.MobileNumber); ok {
		t.Error("Ben was taken off his only session but may still log in")
	}
	if ok, _ := auth.CounterMayLogIn(ctx, db, "+263779999999"); ok {
		t.Error("an unknown number may log in")
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", ada.ID); c.Next() }, middleware.RequireSessionCounter(db))
	r.GET("/counter/sessions", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/counter/sessions/:id/items", func(c *gin.Context) { c.Status(http.StatusOK) })
	for path, want := range map[string]int{
		"/counter/sessions":                     http.StatusOK,
		"/counter/sessions/" + mine + "/items":  http.StatusOK,
		"/counter/sessions/" + other + "/items": http.StatusForbidden,
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Errorf("%s: %d, want %d", path, w.Code, want)
		}
	}
}
