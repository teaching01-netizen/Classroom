package service

import (
	"context"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
)

// DashboardViewService wraps db.DashboardViewRepository to decouple API handlers from the DB layer.
type DashboardViewService struct {
	repo db.DashboardViewRepository
}

// NewDashboardViewService creates a DashboardViewService.
func NewDashboardViewService(repo db.DashboardViewRepository) *DashboardViewService {
	return &DashboardViewService{repo: repo}
}

// List returns all saved dashboard views.
func (s *DashboardViewService) List(ctx context.Context) ([]domain.SavedDashboardView, error) {
	return s.repo.List(ctx)
}

// GetByID returns a single saved dashboard view by ID.
func (s *DashboardViewService) GetByID(ctx context.Context, id int64) (*domain.SavedDashboardView, error) {
	return s.repo.GetByID(ctx, id)
}

// Create creates a new saved dashboard view.
func (s *DashboardViewService) Create(ctx context.Context, name string, filters domain.DashboardFilters) (*domain.SavedDashboardView, error) {
	return s.repo.Create(ctx, name, filters)
}

// Update updates an existing saved dashboard view.
func (s *DashboardViewService) Update(ctx context.Context, id int64, name string, filters domain.DashboardFilters) (*domain.SavedDashboardView, error) {
	return s.repo.Update(ctx, id, name, filters)
}

// Delete deletes a saved dashboard view.
func (s *DashboardViewService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

// Touch updates the last_used_at timestamp for a view.
func (s *DashboardViewService) Touch(ctx context.Context, id int64) error {
	return s.repo.Touch(ctx, id)
}
