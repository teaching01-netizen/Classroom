package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"qr-command-center/internal/api"
	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
	"qr-command-center/internal/scraper"
	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

// Compile-time check: *warwick.ClassroomClient satisfies service.TeacherDataProvider.
var _ service.TeacherDataProvider = (*warwick.ClassroomClient)(nil)

// ServerDeps holds all wired-up components needed to run the server.
type ServerDeps struct {
	Router       *chi.Mux
	RateLimiters *api.RateLimiters
	Background   BackgroundRuntime
	DBPool       *pgxpool.Pool
	EventHub     *service.EventHub
}

func (d *ServerDeps) Close() {
	if d == nil {
		return
	}
	if d.EventHub != nil {
		d.EventHub.Close()
	}
	if d.DBPool != nil {
		d.DBPool.Close()
	}
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

	var qrClient *warwick.WarwickQrClient
	var classroomClient *warwick.ClassroomClient
	if sessionPool != nil {
		qrClient = warwick.NewWarwickQrClientFromPool(sessionPool, warwick.TierQR)
		qrClient.SetBaseURL(cfg.WarwickBaseURL)
		qrClient.SetTransport(sharedTransport)
	}

	// Database
	dbPool, err := db.NewPool(cfg.DatabaseURL, cfg.ServerlessEnabled)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	if err := db.RunMigrations(cfg.DatabaseURL); err != nil {
		dbPool.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	slog.Info("Connected to database")

	eventHub := service.NewEventHub(256, 256)
	repository := db.NewPgRoomRepository(dbPool)
	rm := service.NewRoomManagerWithEventHub(qrClient, repository, eventHub)

	if err := rm.LoadRoomsFromDB(); err != nil {
		eventHub.Close()
		dbPool.Close()
		return nil, fmt.Errorf("load rooms from database: %w", err)
	}
	if cfg.ServerlessEnabled {
		recoveryCtx, recoveryCancel := context.WithTimeout(ctx, 10*time.Second)
		if err := rm.RecoverLoadedRooms(recoveryCtx); err != nil {
			recoveryCancel()
			eventHub.Close()
			dbPool.Close()
			return nil, fmt.Errorf("recover persisted room states: %w", err)
		}
		recoveryCancel()
	}

	favRepo := db.NewPgFavouriteRepository(dbPool)
	favSvc := service.NewFavouriteService(favRepo)
	viewRepo := db.NewPgDashboardViewRepository(dbPool)
	viewSvc := service.NewDashboardViewService(viewRepo)

	if sessionPool != nil {
		classroomClient = warwick.NewClassroomClientFromPool(sessionPool, warwick.TierTeacher)
		classroomClient.SetBaseURL(cfg.WarwickBaseURL)
		classroomClient.SetTransport(sharedTransport)
		if cfg.UserID != "" {
			classroomClient.SetUserID(cfg.UserID)
		}
		classroomClient.SetRateLimiter(rate.NewLimiter(rate.Limit(cfg.ReportRatePerSecond), cfg.ReportRateBurst))
		classroomClient.SetReportConcurrency(cfg.ReportConcurrency)
		classroomClient.SetCourseDetailConcurrency(cfg.CourseDetailConcurrency)
	}

	snapshotRepository := db.NewSnapshotRepository(dbPool)
	snapshotRuntimeEnabled := cfg.Scraper.Enabled || cfg.Scraper.SnapshotReadsEnabled
	if snapshotRuntimeEnabled {
		warnIfSnapshotPoolUndersized(
			dbPool.Config().MaxConns,
			cfg.Scraper.BaselineConcurrency,
		)
	}
	var scheduler *scraper.Scheduler
	var snapshotProvider *service.SnapshotProvider
	persistentWorkers := make([]service.ManagedWorker, 0, 1)
	alwaysOnWorkers := make([]service.ManagedWorker, 0, 1)
	if snapshotRuntimeEnabled {
		if classroomClient == nil {
			eventHub.Close()
			dbPool.Close()
			return nil, errors.New("snapshot runtime requires an initialized Warwick classroom client")
		}
		now := time.Now().UTC()
		if err := snapshotRepository.SeedHost(ctx, db.HostStateSeed{
			Host:                      cfg.Scraper.Host,
			BaselineRequestsPerSecond: cfg.Scraper.BaselineRequestsPerSecond,
			Burst:                     cfg.Scraper.Burst,
			BaselineConcurrency:       cfg.Scraper.BaselineConcurrency,
			Now:                       now,
		}); err != nil {
			eventHub.Close()
			dbPool.Close()
			return nil, fmt.Errorf("seed scraper host: %w", err)
		}
		if err := snapshotRepository.Seed(ctx, rootSnapshotSeeds(cfg.Scraper.Host, now)); err != nil {
			eventHub.Close()
			dbPool.Close()
			return nil, fmt.Errorf("seed scraper roots: %w", err)
		}

		source := warwick.NewSnapshotSource(classroomClient, cfg.Scraper.ResponseBodyLimit)
		source.SetHTTPTraceSampleRate(cfg.Scraper.HTTPTraceSampleRate)
		hostController := scraper.NewHostController(
			snapshotRepository,
			cfg.Scraper.FetchTimeout+cfg.Scraper.PermitGrace,
		)
		coordinator := scraper.NewCoordinator(
			source,
			snapshotRepository,
			hostController,
			scraper.CoordinatorConfig{
				FetchTimeout:          cfg.Scraper.FetchTimeout,
				CanonicalPayloadLimit: cfg.Scraper.CanonicalPayloadLimit,
			},
		)
		scheduler = scraper.NewScheduler(
			snapshotRepository,
			hostController,
			coordinator,
			scraper.SchedulerConfig{
				WorkerID:            uuid.NewString(),
				MaxConcurrency:      cfg.Scraper.BaselineConcurrency,
				PrefetchFactor:      cfg.Scraper.ClaimPrefetchFactor,
				LeaseDuration:       cfg.Scraper.LeaseDuration,
				CommitGrace:         cfg.Scraper.CommitGrace,
				TickLimit:           cfg.Scraper.TickLimit,
				PollInterval:        5 * time.Second,
				RefreshPollInterval: 100 * time.Millisecond,
				RefreshPollMax:      500 * time.Millisecond,
				SnapshotRetention:   cfg.Scraper.SnapshotRetention,
				RunRetention:        cfg.Scraper.RunRetention,
				PruneBatchSize:      1000,
			},
		)
		snapshotProvider = service.NewSnapshotProvider(
			snapshotRepository,
			scheduler,
			cfg.Scraper.Host,
			time.Now,
		)
		listener := service.NewSnapshotNotificationListener(
			cfg.DatabaseURL,
			snapshotRepository,
			snapshotRepository,
			eventHub,
		)
		persistentWorkers = append(persistentWorkers, managedWorkerFunc(func(workerCtx context.Context) {
			if err := listener.Run(workerCtx); err != nil && workerCtx.Err() == nil {
				slog.Warn("snapshot_notification_listener_stopped", "error", err)
			}
		}))
		if cfg.Scraper.Enabled && !cfg.ServerlessEnabled {
			alwaysOnWorkers = append(alwaysOnWorkers, scheduler)
		}
	}

	var teacherService *service.TeacherService
	if cfg.Scraper.SnapshotReadsEnabled {
		if snapshotProvider == nil || scheduler == nil {
			eventHub.Close()
			dbPool.Close()
			return nil, errors.New("snapshot reads enabled without repository and refresher wiring")
		}
		teacherService = service.NewTeacherServiceWithDependencies(
			snapshotProvider,
			snapshotProvider,
			classroomClient,
			scheduler,
			cfg.ReportConcurrency,
			true,
		)
	} else {
		if classroomClient == nil {
			eventHub.Close()
			dbPool.Close()
			return nil, errors.New("live teacher reads require an initialized Warwick classroom client")
		}
		teacherService = service.NewTeacherService(
			classroomClient,
			classroomClient,
			cfg.ReportConcurrency,
		)
	}

	idleHandlers := []service.IdleHandler{
		service.IdleHandlerFunc(func(context.Context) error {
			idleCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return rm.StopAllActiveRooms(idleCtx)
		}),
		service.IdleHandlerFunc(func(context.Context) error {
			sharedTransport.CloseIdleConnections()
			return nil
		}),
	}
	activityController := service.NewActivityController(cfg.ServerlessIdleGrace, nil, idleHandlers)
	var activityRecorder service.ActivityRecorder
	if cfg.ServerlessEnabled {
		activityRecorder = activityController
	}
	router, rateLimiters := api.NewRouter(rm, teacherService, favSvc, viewSvc, api.RouterOptions{
		WSMaxConns:       cfg.WSMaxConns,
		CORSOrigin:       cfg.CORSOrigin,
		ActivityRecorder: activityRecorder,
		EventHub:         eventHub,
		SnapshotMetadata: func() service.SnapshotMetadataStore {
			if snapshotRuntimeEnabled {
				return snapshotRepository
			}
			return nil
		}(),
		ScraperRunner: scheduler,
		ScraperStatus: func() api.ScraperStatusReader {
			if snapshotRuntimeEnabled {
				return snapshotRepository
			}
			return nil
		}(),
		ScraperHost:      cfg.Scraper.Host,
		ScraperTickLimit: cfg.Scraper.TickLimit,
		ScraperToken:     cfg.Scraper.TriggerToken,
	})

	return &ServerDeps{
		Router:       router,
		RateLimiters: rateLimiters,
		Background: BackgroundRuntime{
			Controller: activityController,
			Persistent: persistentWorkers,
			AlwaysOn:   alwaysOnWorkers,
		},
		DBPool:   dbPool,
		EventHub: eventHub,
	}, nil
}

func snapshotPoolMinimum(maxScrapeConcurrency int) int32 {
	if maxScrapeConcurrency < 1 {
		maxScrapeConcurrency = 1
	}
	// Reserve eight connections for API traffic, LISTEN, health checks, and
	// short operational bursts, in addition to the maximum scrape workers.
	return int32(8 + maxScrapeConcurrency)
}

func warnIfSnapshotPoolUndersized(maxConnections int32, maxScrapeConcurrency int) bool {
	minimum := snapshotPoolMinimum(maxScrapeConcurrency)
	if maxConnections >= minimum {
		return false
	}
	slog.Warn(
		"snapshot_database_pool_below_recommended_minimum",
		"configured_max_connections",
		maxConnections,
		"recommended_minimum",
		minimum,
		"scrape_concurrency",
		maxScrapeConcurrency,
	)
	return true
}

func rootSnapshotSeeds(host string, now time.Time) []domain.TargetSeed {
	refs := []domain.TargetRef{
		{
			Host:        host,
			Kind:        domain.SnapshotCourseCatalog,
			ResourceKey: "catalog",
		},
		{
			Host:        host,
			Kind:        domain.SnapshotStudentProfiles,
			ResourceKey: "profiles",
		},
	}
	seeds := make([]domain.TargetSeed, 0, len(refs))
	for _, ref := range refs {
		policy := scraper.PolicyFor(ref.Kind, "")
		seeds = append(seeds, domain.TargetSeed{
			Ref:             ref,
			InitialInterval: policy.Initial,
			MinInterval:     policy.Min,
			MaxInterval:     policy.Max,
			MaxServeAge:     policy.MaxServeAge,
			NextRunAt:       now,
		})
	}
	return seeds
}
