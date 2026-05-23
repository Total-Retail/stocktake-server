package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/totalretail/stocktake/internal/auth"
	"github.com/totalretail/stocktake/internal/config"
	"github.com/totalretail/stocktake/internal/counting"
	"github.com/totalretail/stocktake/internal/ls"
	"github.com/totalretail/stocktake/internal/reporting"
	"github.com/totalretail/stocktake/internal/session"
	"github.com/totalretail/stocktake/internal/settings"
	"github.com/totalretail/stocktake/internal/sms"
	"github.com/totalretail/stocktake/internal/store"
	"github.com/totalretail/stocktake/internal/variance"
	"github.com/totalretail/stocktake/internal/ws"
	"github.com/totalretail/stocktake/pkg/middleware"
	"gorm.io/gorm"
)

type Server struct {
	cfg    *config.Config
	router *gin.Engine
}

func New(cfg *config.Config, db *gorm.DB) *Server {
	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		panic("invalid REDIS_URL: " + err.Error())
	}
	rdb := redis.NewClient(opt)

	hub := ws.NewHub()

	// Services
	authSvc      := auth.NewService(db, rdb, cfg.JWTSecret, cfg.OTPExpiryMinutes, cfg.OTPMaxRequests)
	smsSvc       := sms.NewClient(cfg.SMSBaseURL, cfg.SMSAPIKey)
	lsClient     := ls.NewClient(cfg.LSBaseURL, cfg.LSCompanyID, cfg.LSUsername, cfg.LSPassword)
	storeSvc     := store.NewService(db)
	sessionSvc   := session.NewService(db, lsClient, hub)
	countingSvc  := counting.NewService(db)
	varianceSvc  := variance.NewService(db)
	reportSvc    := reporting.NewService(db)
	settingsSvc  := settings.NewService(db)

	// Handlers
	authHandler      := auth.NewHandler(authSvc, smsSvc, db, cfg.CounterTokenHours, cfg.AdminTokenHours)
	adminUserHandler := auth.NewAdminUserHandler(db)
	storeHandler     := store.NewHandler(storeSvc)
	sessionHandler   := session.NewHandler(sessionSvc, authSvc, smsSvc, cfg.CounterTokenHours, cfg.ExportDir)
	countingHandler  := counting.NewHandler(countingSvc, hub)
	varianceHandler  := variance.NewHandler(varianceSvc, cfg.VarianceTolerancePct)
	reportingHandler := reporting.NewHandler(reportSvc)
	settingsHandler  := settings.NewHandler(settingsSvc)

	router := gin.Default()
	router.Use(corsMiddleware())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := router.Group("/api/v1")

	// Public routes
	authHandler.RegisterRoutes(api)

	// Admin-authenticated routes
	adminRoutes := api.Group("", middleware.RequireAuth(authSvc, auth.TokenAdmin))
	storeHandler.RegisterRoutes(adminRoutes)
	sessionHandler.RegisterRoutes(adminRoutes)
	varianceHandler.RegisterRoutes(adminRoutes)
	reportingHandler.RegisterRoutes(adminRoutes)
	adminUserHandler.RegisterAdminUserRoutes(adminRoutes)
	settingsHandler.RegisterRoutes(adminRoutes)

	// Counter-authenticated routes
	counterRoutes := api.Group("", middleware.RequireAuth(authSvc, auth.TokenCounter))
	countingHandler.RegisterRoutes(counterRoutes)
	sessionHandler.RegisterCounterRoutes(counterRoutes)

	// WebSocket (admin only)
	router.GET("/ws/sessions/:id", middleware.RequireAuth(authSvc, auth.TokenAdmin), hub.ServeWS)

	return &Server{cfg: cfg, router: router}
}

func (s *Server) Run() error {
	return s.router.Run(s.cfg.ServerAddr)
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
