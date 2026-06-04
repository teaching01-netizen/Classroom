import React, { useState, useCallback } from 'react';
import { useAbsenceDashboard } from '../hooks/useAbsenceDashboard';
import { useDashboardViews } from '../hooks/useDashboardViews';
import { useDashboardFiltersStore } from '../store/useDashboardFiltersStore';
import { DashboardKPIRow } from '../components/dashboard/DashboardKPIRow';
import { AtRiskCallout } from '../components/dashboard/AtRiskCallout';
import { FilterBar } from '../components/dashboard/FilterBar';
import { AbsenceMatrix } from '../components/dashboard/AbsenceMatrix';

export function AbsenceDashboard() {
  const { data, loading, error, refetch } = useAbsenceDashboard();
  const { views, isLoading: viewsLoading, error: viewsError, createView, updateView, deleteView, touchView } = useDashboardViews();
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

  if (loading) {
    return (
      <div style={{ padding: 'var(--space-8, 32px)', textAlign: 'center' }}>
        <div style={{
          width: '32px',
          height: '32px',
          border: '3px solid var(--color-border, #DCDBDD)',
          borderTopColor: 'var(--color-primary-600, #276BF0)',
          borderRadius: '50%',
          animation: 'spin 0.8s linear infinite',
          margin: '0 auto var(--space-4, 16px)',
        }} />
        <p style={{ color: 'var(--color-text-secondary, #4F5056)' }}>Loading dashboard...</p>
        <p style={{ fontSize: '0.75rem', color: 'var(--color-text-muted, #696A6C)', marginTop: '8px' }}>
          Fetching courses and attendance data from Warwick...
        </p>
        <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ padding: 'var(--space-8, 32px)' }}>
        <div style={{
          padding: 'var(--space-6, 24px)',
          background: 'color-mix(in srgb, var(--color-danger, #9A3D4A) 12%, transparent)',
          color: 'var(--color-danger, #9A3D4A)',
          borderRadius: 'var(--radius-md, 8px)',
        }}>
          <p style={{ fontWeight: '500', marginBottom: '8px' }}>Failed to load dashboard</p>
          <p style={{ fontSize: '0.875rem', marginBottom: '16px', fontFamily: 'monospace', wordBreak: 'break-all' }}>{error}</p>
          <button
            onClick={refetch}
            style={{
              padding: '8px 16px',
              borderRadius: 'var(--radius-md, 8px)',
              border: '1px solid var(--color-danger, #9A3D4A)',
              background: 'transparent',
              color: 'var(--color-danger, #9A3D4A)',
              cursor: 'pointer',
              fontWeight: '500',
              fontSize: '0.875rem',
            }}
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  return (
    <div style={{ padding: 'var(--space-8, 32px)' }}>
      {viewsError && (
        <div style={{
          padding: 'var(--space-3, 12px) var(--space-4, 16px)',
          background: 'color-mix(in srgb, var(--color-warning, #7A631C) 10%, transparent)',
          color: 'var(--color-warning, #7A631C)',
          borderRadius: 'var(--radius-md, 8px)',
          marginBottom: 'var(--space-4, 16px)',
          fontSize: '0.8125rem',
        }}>
          Saved views unavailable: {viewsError}
        </div>
      )}

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
            Absence Dashboard
          </h2>
          <p style={{
            fontSize: '0.875rem',
            color: 'var(--color-text-secondary, #4F5056)',
          }}>
            Cross-course view of at-risk students
            {data?.generatedAt && (
              <span style={{ fontSize: '0.75rem', color: 'var(--color-text-muted, #696A6C)', marginLeft: '8px' }}>
                Generated {new Date(data.generatedAt).toLocaleTimeString()}
              </span>
            )}
          </p>
        </div>

        <button
          onClick={refetch}
          style={{
            padding: '8px 16px',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--color-border, #DCDBDD)',
            background: 'var(--color-bg, #FFFFFF)',
            color: 'var(--color-text-secondary, #4F5056)',
            cursor: 'pointer',
            fontWeight: '500',
            fontSize: '0.875rem',
            transition: 'border-color 0.2s',
          }}
          onMouseEnter={(e) => { e.currentTarget.style.borderColor = 'var(--color-border-strong, #CFCFD9)'; }}
          onMouseLeave={(e) => { e.currentTarget.style.borderColor = 'var(--color-border, #DCDBDD)'; }}
        >
          Refresh
        </button>
      </div>

      {!data && (
        <div style={{
          padding: 'var(--space-4, 16px)',
          background: 'color-mix(in srgb, var(--color-warning, #7A6D1C) 10%, transparent)',
          color: 'var(--color-warning, #7A631C)',
          borderRadius: 'var(--radius-md, 8px)',
          marginBottom: 'var(--space-6, 24px)',
          fontSize: '0.875rem',
        }}>
          No data returned from server. Check the browser console (F12) and server logs for details.
        </div>
      )}

      <DashboardKPIRow data={data} />

      <AtRiskCallout students={data?.topAtRisk} />

      <FilterBar
        views={views}
        activeViewId={activeViewId}
        onLoadView={handleLoadView}
        onSaveView={handleSaveView}
        onDeleteView={handleDeleteView}
      />

      <AbsenceMatrix students={data?.students} sessions={data?.sessions} />

      {/* Debug info */}
      <div style={{
        marginTop: 'var(--space-8, 32px)',
        padding: 'var(--space-4, 16px)',
        background: 'var(--color-bg-subtle, #F5F5F5)',
        borderRadius: 'var(--radius-md, 8px)',
        fontSize: '0.75rem',
        color: 'var(--color-text-muted, #696A6C)',
        fontFamily: 'monospace',
      }}>
        <div>Students: {data?.students?.length ?? 0} | Sessions: {data?.sessions?.length ?? 0} | Courses: {data?.totalCourses ?? 0} | At Risk: {data?.atRiskCount ?? 0}</div>
      </div>
    </div>
  );
}
