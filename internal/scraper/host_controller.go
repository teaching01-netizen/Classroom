package scraper

import (
	"context"
	"errors"
	"strings"
	"time"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
)

type HostPermitStore interface {
	AcquireHostPermit(context.Context, db.AcquireHostPermitRequest) (domain.PermitDecision, error)
	ReleaseHostPermit(context.Context, int64) error
	ObserveHost(context.Context, domain.HostObservation) error
}

type HostController struct {
	store     HostPermitStore
	permitTTL time.Duration
}

func NewHostController(store HostPermitStore, permitTTL time.Duration) *HostController {
	if store == nil {
		panic("HostController: store must not be nil")
	}
	if permitTTL <= 0 {
		panic("HostController: permit TTL must be positive")
	}
	return &HostController{store: store, permitTTL: permitTTL}
}

func (c *HostController) Acquire(
	ctx context.Context,
	target domain.ScrapeTarget,
	workerID string,
	now time.Time,
) (domain.PermitDecision, error) {
	if target.ID <= 0 || target.LeaseGeneration <= 0 {
		return domain.PermitDecision{}, domain.ErrLeaseLost
	}
	if strings.TrimSpace(workerID) == "" {
		return domain.PermitDecision{}, errors.New("host permit worker ID is required")
	}
	return c.store.AcquireHostPermit(ctx, db.AcquireHostPermitRequest{
		Host:            target.Ref.Host,
		TargetID:        target.ID,
		WorkerID:        workerID,
		LeaseGeneration: target.LeaseGeneration,
		Now:             now,
		TTL:             c.permitTTL,
	})
}

func (c *HostController) Release(ctx context.Context, permit *domain.HostPermit) error {
	if permit == nil {
		return nil
	}
	return c.store.ReleaseHostPermit(ctx, permit.ID)
}

func (c *HostController) Observe(ctx context.Context, observation domain.HostObservation) error {
	return c.store.ObserveHost(ctx, observation)
}
