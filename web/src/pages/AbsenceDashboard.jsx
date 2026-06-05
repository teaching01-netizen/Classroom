import React, { useState, useCallback } from 'react';
import { useAbsenceDashboard } from '../hooks/useAbsenceDashboard';
import { useDashboardViews } from '../hooks/useDashboardViews';
import { useCourses } from '../hooks/useCourses';
import { useDashboardFiltersStore } from '../store/useDashboardFiltersStore';
import { DashboardKPIRow } from '../components/dashboard/DashboardKPIRow';
import { FilterBar } from '../components/dashboard/FilterBar';
import { AbsenceList } from '../components/dashboard/AbsenceList';

export function AbsenceDashboard() {
  const { data, loading, error, loadDashboard } = useAbsenceDashboard();
  const { views, createView, updateView, deleteView, touchView } = useDashboardViews();
  const { courses, isLoading: coursesLoading } = useCourses();
  const loadView = useDashboardFiltersStore((s) => s.loadView);
  const filters = useDashboardFiltersStore((s) => s.filters);
  const [activeViewId, setActiveViewId] = useState(null);

  const handleLoadView = useCallback((view) => {
    setActiveViewId(view.id);
    loadView(view);
    touchView(view.id);
  }, [loadView, touchView]);

  const handleSaveView = useCallback(async (name) => {
    try {
      if (activeViewId) {
        await updateView(activeViewId, name, filters);
      } else {
        const created = await createView(name, filters);
        setActiveViewId(created.id);
      }
    } catch (err) {
      console.error('[Dashboard] Failed to save view:', err);
    }
  }, [activeViewId, filters, createView, updateView]);

  const handleDeleteView = useCallback(async (id) => {
    try {
      await deleteView(id);
      if (activeViewId === id) {
        setActiveViewId(null);
      }
    } catch (err) {
      console.error('[Dashboard] Failed to delete view:', err);
    }
  }, [activeViewId, deleteView]);

  const handleLoadDashboard = useCallback(() => {
    loadDashboard(filters);
  }, [loadDashboard, filters]);

  return (
    <div style={{
      maxWidth: '960px',
      margin: '0 auto',
      padding: 'var(--space-8, 32px)',
    }}>
      <div style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        marginBottom: 'var(--space-6, 24px)',
        flexWrap: 'wrap',
        gap: 'var(--space-4, 16px)',
      }}>
        <div>
          <h2 style={{
            fontSize: '1.5rem',
            fontWeight: '600',
            color: 'var(--color-text-primary, #111113)',
            marginBottom: 'var(--space-1, 4px)',
          }}>
            Student Absence Alerts
          </h2>
          <p style={{
            fontSize: '0.875rem',
            color: 'var(--color-text-secondary, #4F5056)',
          }}>
            View absences per student — filter by course, set thresholds, then load
          </p>
        </div>
      </div>

      <FilterBar
        courses={courses}
        coursesLoading={coursesLoading}
        views={views}
        activeViewId={activeViewId}
        onLoadView={handleLoadView}
        onSaveView={handleSaveView}
        onDeleteView={handleDeleteView}
        onLoadDashboard={handleLoadDashboard}
        dashboardLoading={loading}
      />

      {error && (
        <div style={{
          padding: 'var(--space-4, 16px) var(--space-5, 20px)',
          background: 'color-mix(in srgb, var(--color-danger, #9A3D4A) 12%, transparent)',
          color: 'var(--color-danger, #9A3D4A)',
          borderRadius: 'var(--radius-md, 8px)',
          marginBottom: 'var(--space-6, 24px)',
          fontSize: '0.875rem',
        }}>
          <p style={{ fontWeight: '500', marginBottom: '4px' }}>Failed to load dashboard</p>
          <p style={{ fontFamily: 'monospace', wordBreak: 'break-all', fontSize: '0.8125rem' }}>{error}</p>
        </div>
      )}

      {loading && (
        <div style={{
          textAlign: 'center',
          padding: 'var(--space-8, 32px)',
          color: 'var(--color-text-secondary, #4F5056)',
        }}>
          <div style={{
            width: '32px',
            height: '32px',
            border: '3px solid var(--color-border, #DCDBDD)',
            borderTopColor: 'var(--color-primary-600, #276BF0)',
            borderRadius: '50%',
            animation: 'spin 0.8s linear infinite',
            margin: '0 auto var(--space-4, 16px)',
          }} />
          <p>Loading dashboard data from Warwick...</p>
          <p style={{ fontSize: '0.75rem', color: 'var(--color-text-muted, #696A6C)', marginTop: '4px' }}>
            Fetching course details and computing attendance reports
          </p>
          <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
        </div>
      )}

      {!loading && !error && data && (
        <>
          <DashboardKPIRow data={data} />
          {filters.wCodes.length > 0 && (
            <div style={{
              fontSize: '0.8125rem',
              color: 'var(--color-text-secondary, #4F5056)',
              marginBottom: 'var(--space-3, 12px)',
              padding: 'var(--space-2, 8px) var(--space-3, 12px)',
              background: 'color-mix(in srgb, var(--color-primary-600, #276BF0) 6%, transparent)',
              borderRadius: 'var(--radius-sm, 4px)',
              border: '1px solid color-mix(in srgb, var(--color-primary-600, #276BF0) 15%, transparent)',
            }}>
              Showing {data.students?.length || 0} of {filters.wCodes.length} specified WCode{filters.wCodes.length !== 1 ? 's' : ''}
            </div>
          )}
          <AbsenceList students={data?.students} sessions={data?.sessions} />
        </>
      )}

      {!loading && !error && !data && (
        <div style={{
          textAlign: 'center',
          padding: 'var(--space-12, 48px) var(--space-8, 32px)',
          color: 'var(--color-text-muted, #696A6C)',
        }}>
          <div style={{
            width: '48px',
            height: '48px',
            borderRadius: '50%',
            background: 'var(--color-bg-hover, #F1F2F4)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            margin: '0 auto var(--space-4, 16px)',
            fontSize: '1.5rem',
          }}>
            📊
          </div>
          <p style={{ fontSize: '1rem', fontWeight: '500', color: 'var(--color-text-secondary, #4F5056)', marginBottom: '8px' }}>
            Configure your dashboard above
          </p>
          <p style={{ fontSize: '0.875rem' }}>
            Select courses, set filters, then click <strong>Load Dashboard</strong>
          </p>
        </div>
      )}
    </div>
  );
}
