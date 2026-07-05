package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"qr-command-center/internal/api"
	"qr-command-center/internal/cache"
	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

// Compile-time check: *warwick.ClassroomClient satisfies service.TeacherDataProvider.
var _ service.TeacherDataProvider = (*warwick.ClassroomClient)(nil)

// ServerDeps holds all wired-up components needed to run the server.
type ServerDeps struct {
	Router          *chi.Mux
	RateLimiters    *api.RateLimiters
	Refresher       *service.DataRefresher
	ReportPersister *service.ReportPersister
	DBPool          *pgxpool.Pool
}

// Wire creates all dependencies from config and returns a ready-to-use ServerDeps.
// It connects to the database, creates the session pool, and wires up all services.
func Wire(ctx context.Context, cfg Config) (*ServerDeps, error) {
	// Session pool with traffic-tier isolation
	var sessionPool *warwick.SessionPool
	sharedTransport := warwick.NewSharedTransport(cfg.ConnsPerHost)
	pool, err := warwick.NewSessionPool(cfg.Email, cfg.Password, cfg.WarwickBaseURL+"/admin/",
		cfg.QRSessions, cfg.TeacherSessions, cfg.InteractiveSessions, sharedTransport)
	if err != nil {
		slog.Warn("Failed to create Warwick session pool; will retry on demand", "error", err)
	} else {
		sessionPool = pool
	}

	sharedCache := cache.New()

	var qrClient *warwick.WarwickQrClient
	var classroomClient *warwick.ClassroomClient
	var refresher *service.DataRefresher
	if sessionPool != nil {
		qrClient = warwick.NewWarwickQrClientFromPool(sessionPool, warwick.TierQR)
		qrClient.SetBaseURL(cfg.WarwickBaseURL)
	}

	// Database
	dbPool, err := db.NewPool(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	if err := db.RunMigrations(cfg.DatabaseURL); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	slog.Info("Connected to database")

	repository := db.NewPgRoomRepository(dbPool)
	rm := service.NewRoomManager(qrClient, repository)

	if err := rm.LoadRoomsFromDB(); err != nil {
		return nil, fmt.Errorf("load rooms from database: %w", err)
	}

	favRepo := db.NewPgFavouriteRepository(dbPool)
	favSvc := service.NewFavouriteService(favRepo)
	viewRepo := db.NewPgDashboardViewRepository(dbPool)
	viewSvc := service.NewDashboardViewService(viewRepo)

	var sessionCheckinRepo db.SessionCheckinRepository
	var reportPersister *service.ReportPersister

	if sessionPool != nil {
		sessionCheckinRepo = db.NewPgSessionCheckinRepository(dbPool)
		classroomClient = warwick.NewClassroomClientFromPool(sessionPool, warwick.TierTeacher, sharedCache, sessionCheckinRepo)
		classroomClient.SetBaseURL(cfg.WarwickBaseURL)
		if cfg.UserID != "" {
			classroomClient.SetUserID(cfg.UserID)
		}
		classroomClient.SetRateLimiter(rate.NewLimiter(rate.Limit(2), 2))
		reportCache := cache.New()
		classroomClient.SetReportCache(reportCache)
		refresher = service.NewDataRefresher(classroomClient, cfg.CacheInterval)

		// Pre-warm
		if err := sessionPool.SetPreWarmSize(cfg.PreWarmSessions); err != nil {
			slog.Warn("failed to configure prewarm pool size, prewarmer disabled", "error", err, "size", cfg.PreWarmSessions)
		} else {
			prewarmClient := warwick.NewClassroomClientFromPool(sessionPool, warwick.TierPreWarm, cache.New(), sessionCheckinRepo)
			prewarmClient.SetBaseURL(cfg.WarwickBaseURL)
			if cfg.UserID != "" {
				prewarmClient.SetUserID(cfg.UserID)
			}
			prewarmClient.SetRateLimiter(rate.NewLimiter(rate.Limit(2), 2))
			prewarmer := service.NewSessionPreWarmer(prewarmClient, prewarmClient, sessionCheckinRepo, cfg.PreWarmInterval)
			go func() {
				prewarmer.Run(ctx)
			}()
			slog.Info("session prewarmer started", "prewarm_sessions", cfg.PreWarmSessions, "interval", cfg.PreWarmInterval)
		}

		// Report persister
		attendanceReportRepo := db.NewPgAttendanceReportRepository(dbPool)
		reportPersister = service.NewReportPersister(attendanceReportRepo, reportCache, 100)
		metrics.SetQueueDepthFunc(reportPersister.QueueDepth)
		go func() {
			reportPersister.Run(ctx)
		}()

		// Boot hydration
		hydrator := service.NewReportHydrator(attendanceReportRepo, reportCache)
		hydratorCtx, hydratorCancel := context.WithTimeout(ctx, 10*time.Second)
		if err := hydrator.Hydrate(hydratorCtx, 200); err != nil {
			slog.Warn("initial report hydration failed, will retry on demand", "error", err)
		}
		hydratorCancel()

		// Sync warmup
		warmupCtx, warmupCancel := context.WithTimeout(ctx, 10*time.Second)
		if err := refresher.WarmOnce(warmupCtx); err != nil {
			slog.Warn("initial cache warmup failed, will retry in background", "error", err)
		}
		warmupCancel()
	}

	var defaultFetcher domain.SessionFetcher
	if sessionCheckinRepo != nil && sessionPool != nil {
		dbFetcher := db.NewDBSessionFetcher(sessionCheckinRepo)
		liveFetcher := warwick.NewLiveSessionDataSource(classroomClient)
		defaultFetcher = warwick.NewFallbackSessionDataSource(dbFetcher, liveFetcher)
	} else if classroomClient != nil {
		defaultFetcher = warwick.NewLiveSessionDataSource(classroomClient)
	}
	teacherService := service.NewTeacherService(classroomClient, defaultFetcher)
	router, rateLimiters := api.NewRouter(rm, teacherService, favSvc, sharedCache, refresher, cfg.WSMaxConns, viewSvc, cfg.CORSOrigin)

	return &ServerDeps{
		Router:          router,
		RateLimiters:    rateLimiters,
		Refresher:       refresher,
		ReportPersister: reportPersister,
		DBPool:          dbPool,
	}, nil
}
