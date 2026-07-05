package service

import (
	"context"

	"qr-command-center/internal/db"
)

// FavouriteService wraps db.FavouriteRepository to decouple API handlers from the DB layer.
type FavouriteService struct {
	repo db.FavouriteRepository
}

// NewFavouriteService creates a FavouriteService.
func NewFavouriteService(repo db.FavouriteRepository) *FavouriteService {
	return &FavouriteService{repo: repo}
}

// GetAll returns all favourite course IDs.
func (s *FavouriteService) GetAll(ctx context.Context) ([]string, error) {
	return s.repo.GetAll(ctx)
}

// Add adds a course to favourites.
func (s *FavouriteService) Add(ctx context.Context, courseID string) error {
	return s.repo.Add(ctx, courseID)
}

// Remove removes a course from favourites.
func (s *FavouriteService) Remove(ctx context.Context, courseID string) error {
	return s.repo.Remove(ctx, courseID)
}
