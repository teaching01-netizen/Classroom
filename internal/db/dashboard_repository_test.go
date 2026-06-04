//go:build integration

package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func cleanupDashboardView(t *testing.T, pool *pgxpool.Pool, id int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `DELETE FROM saved_dashboard_views WHERE id = $1`, id)
	if err != nil {
		t.Logf("cleanup failed: %v", err)
	}
}

func TestDashboardViewRepository_Create(t *testing.T) {
	pool := newTestPool(t)
	repo := NewPgDashboardViewRepository(pool)

	filters := domain.DashboardFilters{
		CourseIds: []string{"c1", "c2"},
		Threshold: 3,
		SortBy:    domain.SortByRisk,
	}

	view, err := repo.Create(context.Background(), "Test View", filters)
	require.NoError(t, err)
	require.NotNil(t, view)
	t.Cleanup(func() { cleanupDashboardView(t, pool, view.ID) })

	assert.Greater(t, view.ID, int64(0))
	assert.Equal(t, "Test View", view.Name)
	assert.Equal(t, []string{"c1", "c2"}, view.Filters.CourseIds)
	assert.Equal(t, 3, view.Filters.Threshold)
	assert.False(t, view.LastUsedAt.IsZero())
	assert.False(t, view.CreatedAt.IsZero())
}

func TestDashboardViewRepository_CreateDuplicateName(t *testing.T) {
	pool := newTestPool(t)
	repo := NewPgDashboardViewRepository(pool)

	filters := domain.DefaultDashboardFilters()
	view1, err := repo.Create(context.Background(), "Unique Name", filters)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupDashboardView(t, pool, view1.ID) })

	_, err = repo.Create(context.Background(), "Unique Name", filters)
	assert.Error(t, err)
}

func TestDashboardViewRepository_List(t *testing.T) {
	pool := newTestPool(t)
	repo := NewPgDashboardViewRepository(pool)

	filters := domain.DefaultDashboardFilters()
	view1, err := repo.Create(context.Background(), "List Test 1", filters)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupDashboardView(t, pool, view1.ID) })

	view2, err := repo.Create(context.Background(), "List Test 2", filters)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupDashboardView(t, pool, view2.ID) })

	views, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(views), 2)
}

func TestDashboardViewRepository_GetByID(t *testing.T) {
	pool := newTestPool(t)
	repo := NewPgDashboardViewRepository(pool)

	filters := domain.DashboardFilters{
		CourseIds: []string{"c1"},
		Threshold: 5,
		SortBy:    domain.SortByRateAsc,
	}

	view, err := repo.Create(context.Background(), "Get Test", filters)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupDashboardView(t, pool, view.ID) })

	fetched, err := repo.GetByID(context.Background(), view.ID)
	require.NoError(t, err)
	assert.Equal(t, view.ID, fetched.ID)
	assert.Equal(t, "Get Test", fetched.Name)
	assert.Equal(t, []string{"c1"}, fetched.Filters.CourseIds)
	assert.Equal(t, 5, fetched.Filters.Threshold)
	assert.Equal(t, domain.SortByRateAsc, fetched.Filters.SortBy)
}

func TestDashboardViewRepository_GetByID_NotFound(t *testing.T) {
	pool := newTestPool(t)
	repo := NewPgDashboardViewRepository(pool)

	_, err := repo.GetByID(context.Background(), 999999)
	assert.Error(t, err)
}

func TestDashboardViewRepository_Update(t *testing.T) {
	pool := newTestPool(t)
	repo := NewPgDashboardViewRepository(pool)

	filters := domain.DefaultDashboardFilters()
	view, err := repo.Create(context.Background(), "Original Name", filters)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupDashboardView(t, pool, view.ID) })

	newFilters := domain.DashboardFilters{
		CourseIds: []string{"c3"},
		Threshold: 10,
		SortBy:    domain.SortByRateDesc,
	}

	updated, err := repo.Update(context.Background(), view.ID, "Updated Name", newFilters)
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", updated.Name)
	assert.Equal(t, []string{"c3"}, updated.Filters.CourseIds)
	assert.Equal(t, 10, updated.Filters.Threshold)
}

func TestDashboardViewRepository_Delete(t *testing.T) {
	pool := newTestPool(t)
	repo := NewPgDashboardViewRepository(pool)

	filters := domain.DefaultDashboardFilters()
	view, err := repo.Create(context.Background(), "Delete Test", filters)
	require.NoError(t, err)

	err = repo.Delete(context.Background(), view.ID)
	assert.NoError(t, err)

	_, err = repo.GetByID(context.Background(), view.ID)
	assert.Error(t, err)
}

func TestDashboardViewRepository_Delete_NotFound(t *testing.T) {
	pool := newTestPool(t)
	repo := NewPgDashboardViewRepository(pool)

	err := repo.Delete(context.Background(), 999999)
	assert.Error(t, err)
}

func TestDashboardViewRepository_Touch(t *testing.T) {
	pool := newTestPool(t)
	repo := NewPgDashboardViewRepository(pool)

	filters := domain.DefaultDashboardFilters()
	view, err := repo.Create(context.Background(), "Touch Test", filters)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupDashboardView(t, pool, view.ID) })

	err = repo.Touch(context.Background(), view.ID)
	assert.NoError(t, err)

	fetched, err := repo.GetByID(context.Background(), view.ID)
	require.NoError(t, err)
	assert.True(t, fetched.LastUsedAt.After(view.LastUsedAt) || fetched.LastUsedAt.Equal(view.LastUsedAt))
}

func TestDashboardViewRepository_Touch_NotFound(t *testing.T) {
	pool := newTestPool(t)
	repo := NewPgDashboardViewRepository(pool)

	err := repo.Touch(context.Background(), 999999)
	assert.Error(t, err)
}
