import React from 'react';
import { useDashboardFiltersStore, selectFilterSummary } from '../../store/useDashboardFiltersStore';

export function FilterBar({ views, activeViewId, onLoadView, onSaveView, onDeleteView }) {
  const filters = useDashboardFiltersStore((s) => s.filters);
  const setCourseIds = useDashboardFiltersStore((s) => s.setCourseIds);
  const setThreshold = useDashboardFiltersStore((s) => s.setThreshold);
  const setSortBy = useDashboardFiltersStore((s) => s.setSortBy);
  const setDateRange = useDashboardFiltersStore((s) => s.setDateRange);
  const resetFilters = useDashboardFiltersStore((s) => s.resetFilters);
  const filterSummary = useDashboardFiltersStore(selectFilterSummary);

  const [showSaveDialog, setShowSaveDialog] = React.useState(false);
  const [viewName, setViewName] = React.useState('');
  const [showDeleteConfirm, setShowDeleteConfirm] = React.useState(null);

  const handleSave = () => {
    if (viewName.trim()) {
      onSaveView(viewName.trim());
      setViewName('');
      setShowSaveDialog(false);
    }
  };

  const handleDelete = (id) => {
    onDeleteView(id);
    setShowDeleteConfirm(null);
  };

  return (
    <div style={{
      background: 'var(--color-bg, #FFFFFF)',
      border: '1px solid var(--color-border, #DCDBDD)',
      borderRadius: 'var(--radius-lg, 12px)',
      padding: 'var(--space-4, 16px)',
      marginBottom: 'var(--space-6, 24px)',
    }}>
      {/* Top row: View selector + actions */}
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--space-3, 12px)',
        marginBottom: 'var(--space-3, 12px)',
        flexWrap: 'wrap',
      }}>
        <div style={{ flex: 1, minWidth: '200px' }}>
          <label style={{
            fontSize: '0.75rem',
            fontWeight: '600',
            color: 'var(--color-text-secondary, #4F5056)',
            display: 'block',
            marginBottom: 'var(--space-1, 4px)',
          }}>
            Saved Views
          </label>
          <select
            value={activeViewId || ''}
            onChange={(e) => {
              const id = e.target.value ? Number(e.target.value) : null;
              const view = views.find((v) => v.id === id);
              if (view) onLoadView(view);
            }}
            style={{
              width: '100%',
              padding: '8px 12px',
              borderRadius: 'var(--radius-md, 8px)',
              border: '1px solid var(--color-border, #DCDBDD)',
              background: 'var(--color-bg, #FFFFFF)',
              fontSize: '0.875rem',
              color: 'var(--color-text-primary, #111113)',
              cursor: 'pointer',
            }}
          >
            <option value="">All courses — Default</option>
            {(views || []).map((view) => (
              <option key={view.id} value={view.id}>{view.name}</option>
            ))}
          </select>
        </div>

        <div style={{ display: 'flex', gap: 'var(--space-2, 8px)', alignSelf: 'flex-end' }}>
          {!showSaveDialog ? (
            <button
              onClick={() => setShowSaveDialog(true)}
              style={{
                padding: '8px 16px',
                borderRadius: 'var(--radius-md, 8px)',
                border: '1px solid var(--color-primary-600, #276BF0)',
                background: 'var(--color-primary-600, #276BF0)',
                color: 'var(--color-text-inverse, #FFFFFF)',
                cursor: 'pointer',
                fontSize: '0.8125rem',
                fontWeight: '500',
              }}
            >
              Save View
            </button>
          ) : (
            <div style={{ display: 'flex', gap: 'var(--space-2, 8px)' }}>
              <input
                type="text"
                value={viewName}
                onChange={(e) => setViewName(e.target.value)}
                placeholder="View name..."
                autoFocus
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleSave();
                  if (e.key === 'Escape') setShowSaveDialog(false);
                }}
                style={{
                  padding: '8px 12px',
                  borderRadius: 'var(--radius-md, 8px)',
                  border: '1px solid var(--color-border, #DCDBDD)',
                  fontSize: '0.875rem',
                  width: '180px',
                }}
              />
              <button
                onClick={handleSave}
                disabled={!viewName.trim()}
                style={{
                  padding: '8px 16px',
                  borderRadius: 'var(--radius-md, 8px)',
                  border: '1px solid var(--color-primary-600, #276BF0)',
                  background: viewName.trim() ? 'var(--color-primary-600, #276BF0)' : 'var(--color-bg-hover, #F1F2F4)',
                  color: viewName.trim() ? 'var(--color-text-inverse, #FFFFFF)' : 'var(--color-text-muted, #696A6C)',
                  cursor: viewName.trim() ? 'pointer' : 'not-allowed',
                  fontSize: '0.8125rem',
                  fontWeight: '500',
                }}
              >
                Save
              </button>
              <button
                onClick={() => setShowSaveDialog(false)}
                style={{
                  padding: '8px 12px',
                  borderRadius: 'var(--radius-md, 8px)',
                  border: '1px solid var(--color-border, #DCDBDD)',
                  background: 'transparent',
                  color: 'var(--color-text-secondary, #4F5056)',
                  cursor: 'pointer',
                  fontSize: '0.8125rem',
                }}
              >
                Cancel
              </button>
            </div>
          )}

          {activeViewId && (
            <div style={{ position: 'relative' }}>
              <button
                onClick={() => setShowDeleteConfirm(activeViewId)}
                style={{
                  padding: '8px 12px',
                  borderRadius: 'var(--radius-md, 8px)',
                  border: '1px solid var(--color-border, #DCDBDD)',
                  background: 'transparent',
                  color: 'var(--color-danger, #9A3D4A)',
                  cursor: 'pointer',
                  fontSize: '0.8125rem',
                }}
              >
                Delete
              </button>
              {showDeleteConfirm === activeViewId && (
                <div style={{
                  position: 'absolute',
                  top: '100%',
                  right: 0,
                  marginTop: '4px',
                  padding: 'var(--space-3, 12px)',
                  background: 'var(--color-bg, #FFFFFF)',
                  border: '1px solid var(--color-border, #DCDBDD)',
                  borderRadius: 'var(--radius-md, 8px)',
                  boxShadow: 'var(--shadow-lg, 0 8px 24px rgba(16, 24, 40, 0.15))',
                  zIndex: 10,
                  whiteSpace: 'nowrap',
                }}>
                  <p style={{ fontSize: '0.8125rem', marginBottom: 'var(--space-2, 8px)', color: 'var(--color-text-secondary, #4F5056)' }}>
                    Delete this view?
                  </p>
                  <div style={{ display: 'flex', gap: 'var(--space-2, 8px)' }}>
                    <button
                      onClick={() => handleDelete(activeViewId)}
                      style={{
                        padding: '4px 12px',
                        borderRadius: 'var(--radius-sm, 4px)',
                        border: 'none',
                        background: 'var(--color-danger, #9A3D4A)',
                        color: '#fff',
                        cursor: 'pointer',
                        fontSize: '0.75rem',
                        fontWeight: '500',
                      }}
                    >
                      Yes
                    </button>
                    <button
                      onClick={() => setShowDeleteConfirm(null)}
                      style={{
                        padding: '4px 12px',
                        borderRadius: 'var(--radius-sm, 4px)',
                        border: '1px solid var(--color-border, #DCDBDD)',
                        background: 'transparent',
                        color: 'var(--color-text-secondary, #4F5056)',
                        cursor: 'pointer',
                        fontSize: '0.75rem',
                      }}
                    >
                      No
                    </button>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Filter row */}
      <div style={{
        display: 'flex',
        gap: 'var(--space-3, 12px)',
        flexWrap: 'wrap',
        alignItems: 'flex-end',
      }}>
        <FilterSelect
          label="Sort by"
          value={filters.sortBy}
          onChange={setSortBy}
          options={[
            { value: 'risk', label: 'Most at risk' },
            { value: 'rate-asc', label: 'Attendance ↑' },
            { value: 'rate-desc', label: 'Attendance ↓' },
            { value: 'name', label: 'Name A-Z' },
          ]}
        />

        <FilterInput
          label="Absence threshold"
          type="number"
          value={filters.threshold || ''}
          onChange={(v) => setThreshold(Number(v) || 0)}
          placeholder="0 = default"
          min="0"
        />

        <FilterInput
          label="From date"
          type="date"
          value={filters.dateRange?.from || ''}
          onChange={(v) => {
            const to = filters.dateRange?.to || '';
            setDateRange(v || to ? { from: v, to } : null);
          }}
        />

        <FilterInput
          label="To date"
          type="date"
          value={filters.dateRange?.to || ''}
          onChange={(v) => {
            const from = filters.dateRange?.from || '';
            setDateRange(from || v ? { from, to: v } : null);
          }}
        />

        <div style={{ marginLeft: 'auto', fontSize: '0.8125rem', color: 'var(--color-text-muted, #696A6C)' }}>
          {filterSummary}
        </div>

        <button
          onClick={resetFilters}
          style={{
            padding: '6px 12px',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--color-border, #DCDBDD)',
            background: 'transparent',
            color: 'var(--color-text-secondary, #4F5056)',
            cursor: 'pointer',
            fontSize: '0.8125rem',
          }}
        >
          Reset
        </button>
      </div>
    </div>
  );
}

function FilterSelect({ label, value, onChange, options }) {
  return (
    <div>
      <label style={{
        fontSize: '0.75rem',
        fontWeight: '600',
        color: 'var(--color-text-secondary, #4F5056)',
        display: 'block',
        marginBottom: 'var(--space-1, 4px)',
      }}>
        {label}
      </label>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        style={{
          padding: '8px 12px',
          borderRadius: 'var(--radius-md, 8px)',
          border: '1px solid var(--color-border, #DCDBDD)',
          background: 'var(--color-bg, #FFFFFF)',
          fontSize: '0.875rem',
          color: 'var(--color-text-primary, #111113)',
          cursor: 'pointer',
          minWidth: '140px',
        }}
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>{opt.label}</option>
        ))}
      </select>
    </div>
  );
}

function FilterInput({ label, type, value, onChange, placeholder, min }) {
  return (
    <div>
      <label style={{
        fontSize: '0.75rem',
        fontWeight: '600',
        color: 'var(--color-text-secondary, #4F5056)',
        display: 'block',
        marginBottom: 'var(--space-1, 4px)',
      }}>
        {label}
      </label>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        min={min}
        style={{
          padding: '8px 12px',
          borderRadius: 'var(--radius-md, 8px)',
          border: '1px solid var(--color-border, #DCDBDD)',
          fontSize: '0.875rem',
          width: type === 'date' ? '150px' : '120px',
        }}
      />
    </div>
  );
}
