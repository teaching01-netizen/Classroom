import React, { useState, useMemo } from 'react';
import { useDashboardFiltersStore, selectFilterSummary } from '../../store/useDashboardFiltersStore';

export function FilterBar({ courses, coursesLoading, activeViewId, onLoadView, onSaveView, onDeleteView, onLoadDashboard, dashboardLoading, views }) {
  const filters = useDashboardFiltersStore((s) => s.filters);
  const setCourseIds = useDashboardFiltersStore((s) => s.setCourseIds);
  const setThreshold = useDashboardFiltersStore((s) => s.setThreshold);
  const setSortBy = useDashboardFiltersStore((s) => s.setSortBy);
  const setDateRange = useDashboardFiltersStore((s) => s.setDateRange);
  const resetFilters = useDashboardFiltersStore((s) => s.resetFilters);
  const filterSummary = useDashboardFiltersStore(selectFilterSummary);

  const [showSaveDialog, setShowSaveDialog] = useState(false);
  const [viewName, setViewName] = useState('');
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(null);
  const [courseSearch, setCourseSearch] = useState('');
  const [showCoursePicker, setShowCoursePicker] = useState(false);

  const filteredCourses = useMemo(() => {
    if (!courses) return [];
    if (!courseSearch.trim()) return courses;
    const q = courseSearch.toLowerCase();
    return courses.filter((c) =>
      (c.name || '').toLowerCase().includes(q) ||
      (c.course_id || '').toLowerCase().includes(q)
    );
  }, [courses, courseSearch]);

  const selectedCount = filters.courseIds?.length || 0;

  const toggleCourse = (courseId) => {
    const current = filters.courseIds || [];
    const next = current.includes(courseId)
      ? current.filter((id) => id !== courseId)
      : [...current, courseId];
    setCourseIds(next);
  };

  const selectAll = () => {
    setCourseIds(filteredCourses.map((c) => c.course_id));
  };

  const clearAll = () => {
    setCourseIds([]);
  };

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
      padding: 'var(--space-5, 20px)',
      marginBottom: 'var(--space-6, 24px)',
    }}>
      {/* Saved Views Row */}
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--space-3, 12px)',
        marginBottom: 'var(--space-4, 16px)',
        flexWrap: 'wrap',
      }}>
        <div style={{ flex: 1, minWidth: '200px' }}>
          <label style={labelStyle}>Saved Views</label>
          <select
            value={activeViewId || ''}
            onChange={(e) => {
              const id = e.target.value ? Number(e.target.value) : null;
              const view = (views || []).find((v) => v.id === id);
              if (view) onLoadView(view);
            }}
            style={selectStyle}
          >
            <option value="">— Select a view —</option>
            {(views || []).map((view) => (
              <option key={view.id} value={view.id}>{view.name}</option>
            ))}
          </select>
        </div>

        <div style={{ display: 'flex', gap: 'var(--space-2, 8px)', alignSelf: 'flex-end' }}>
          {!showSaveDialog ? (
            <button onClick={() => setShowSaveDialog(true)} style={btnPrimary}>
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
                style={{ ...inputStyle, width: '180px' }}
              />
              <button onClick={handleSave} disabled={!viewName.trim()} style={btnPrimary}>Save</button>
              <button onClick={() => setShowSaveDialog(false)} style={btnSecondary}>Cancel</button>
            </div>
          )}

          {activeViewId && (
            <div style={{ position: 'relative' }}>
              <button onClick={() => setShowDeleteConfirm(activeViewId)} style={btnDanger}>Delete</button>
              {showDeleteConfirm === activeViewId && (
                <div style={confirmBox}>
                  <p style={{ fontSize: '0.8125rem', marginBottom: '8px', color: 'var(--color-text-secondary, #4F5056)' }}>Delete this view?</p>
                  <div style={{ display: 'flex', gap: '8px' }}>
                    <button onClick={() => handleDelete(activeViewId)} style={btnDangerSmall}>Yes</button>
                    <button onClick={() => setShowDeleteConfirm(null)} style={btnSecondarySmall}>No</button>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Course Picker */}
      <div style={{ marginBottom: 'var(--space-4, 16px)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: 'var(--space-2, 8px)' }}>
          <label style={labelStyle}>Courses</label>
          {selectedCount > 0 && (
            <span style={{
              fontSize: '0.75rem',
              fontWeight: '500',
              color: 'var(--color-primary-600, #276BF0)',
              background: 'color-mix(in srgb, var(--color-primary-600, #276BF0) 10%, transparent)',
              padding: '2px 8px',
              borderRadius: 'var(--radius-sm, 4px)',
            }}>
              {selectedCount} selected
            </span>
          )}
          <button
            onClick={() => setShowCoursePicker(!showCoursePicker)}
            style={{
              fontSize: '0.75rem',
              color: 'var(--color-primary-600, #276BF0)',
              background: 'none',
              border: 'none',
              cursor: 'pointer',
              fontWeight: '500',
              padding: 0,
              marginLeft: 'auto',
            }}
          >
            {showCoursePicker ? 'Collapse' : 'Expand'}
          </button>
        </div>

        {showCoursePicker && (
          <div style={{
            border: '1px solid var(--color-border, #DCDBDD)',
            borderRadius: 'var(--radius-md, 8px)',
            overflow: 'hidden',
          }}>
            <div style={{
              padding: '8px 12px',
              background: 'var(--color-bg-subtle, #F5F5F5)',
              borderBottom: '1px solid var(--color-border, #DCDBDD)',
              display: 'flex',
              gap: '8px',
              alignItems: 'center',
            }}>
              <input
                type="text"
                value={courseSearch}
                onChange={(e) => setCourseSearch(e.target.value)}
                placeholder="Search courses..."
                style={{ ...inputStyle, flex: 1, margin: 0 }}
              />
              <button onClick={selectAll} style={linkBtn}>Select all</button>
              <button onClick={clearAll} style={linkBtn}>Clear</button>
            </div>

            <div style={{ maxHeight: '240px', overflowY: 'auto' }}>
              {coursesLoading ? (
                <div style={{ padding: '16px', textAlign: 'center', color: 'var(--color-text-muted, #696A6C)', fontSize: '0.8125rem' }}>
                  Loading courses...
                </div>
              ) : filteredCourses.length === 0 ? (
                <div style={{ padding: '16px', textAlign: 'center', color: 'var(--color-text-muted, #696A6C)', fontSize: '0.8125rem' }}>
                  No courses found
                </div>
              ) : (
                filteredCourses.map((course) => (
                  <label
                    key={course.course_id}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '10px',
                      padding: '8px 12px',
                      borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
                      cursor: 'pointer',
                      fontSize: '0.8125rem',
                      background: (filters.courseIds || []).includes(course.course_id)
                        ? 'color-mix(in srgb, var(--color-primary-600, #276BF0) 5%, transparent)'
                        : 'transparent',
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={(filters.courseIds || []).includes(course.course_id)}
                      onChange={() => toggleCourse(course.course_id)}
                      style={{ accentColor: 'var(--color-primary-600, #276BF0)', width: '16px', height: '16px' }}
                    />
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{
                        fontWeight: '500',
                        color: 'var(--color-text-primary, #111113)',
                        whiteSpace: 'nowrap',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                      }}>
                        {course.name || course.course_id}
                      </div>
                      <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted, #696A6C)' }}>
                        {course.course_id}
                        {course.total_sessions != null && ` · ${course.total_sessions} sessions`}
                        {course.enrolled_count != null && ` · ${course.enrolled_count} enrolled`}
                      </div>
                    </div>
                    {course.status && (
                      <span style={{
                        fontSize: '0.6875rem',
                        fontWeight: '500',
                        padding: '2px 6px',
                        borderRadius: 'var(--radius-sm, 4px)',
                        background: course.status === 'active'
                          ? 'color-mix(in srgb, var(--color-success, #257348) 10%, transparent)'
                          : 'var(--color-bg-hover, #F1F2F4)',
                        color: course.status === 'active'
                          ? 'var(--color-success, #257348)'
                          : 'var(--color-text-muted, #696A6C)',
                      }}>
                        {course.status}
                      </span>
                    )}
                  </label>
                ))
              )}
            </div>
          </div>
        )}

        {!showCoursePicker && selectedCount > 0 && (
          <p style={{ fontSize: '0.75rem', color: 'var(--color-text-muted, #696A6C)', marginTop: '4px' }}>
            {selectedCount} course{selectedCount !== 1 ? 's' : ''} selected — expand to change
          </p>
        )}
        {!showCoursePicker && selectedCount === 0 && (
          <p style={{ fontSize: '0.75rem', color: 'var(--color-text-muted, #696A6C)', marginTop: '4px' }}>
            All courses (no filter) — expand to select specific courses
          </p>
        )}
      </div>

      {/* Filter Row */}
      <div style={{
        display: 'flex',
        gap: 'var(--space-3, 12px)',
        flexWrap: 'wrap',
        alignItems: 'flex-end',
        marginBottom: 'var(--space-4, 16px)',
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

        <button onClick={resetFilters} style={btnSecondary}>Reset</button>
      </div>

      {/* Load Button + Filter Summary */}
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--space-3, 12px)',
        paddingTop: 'var(--space-3, 12px)',
        borderTop: '1px solid var(--color-border-subtle, #EEEFF1)',
      }}>
        <button
          onClick={onLoadDashboard}
          disabled={dashboardLoading}
          style={{
            padding: '10px 24px',
            borderRadius: 'var(--radius-md, 8px)',
            border: 'none',
            background: dashboardLoading ? 'var(--color-bg-hover, #F1F2F4)' : 'var(--color-primary-600, #276BF0)',
            color: dashboardLoading ? 'var(--color-text-muted, #696A6C)' : 'var(--color-text-inverse, #FFFFFF)',
            cursor: dashboardLoading ? 'not-allowed' : 'pointer',
            fontWeight: '600',
            fontSize: '0.875rem',
            transition: 'background 0.2s',
          }}
        >
          {dashboardLoading ? 'Loading...' : 'Load Dashboard'}
        </button>

        <span style={{ fontSize: '0.8125rem', color: 'var(--color-text-muted, #696A6C)' }}>
          {filterSummary}
        </span>
      </div>
    </div>
  );
}

const labelStyle = {
  fontSize: '0.75rem',
  fontWeight: '600',
  color: 'var(--color-text-secondary, #4F5056)',
  display: 'block',
  marginBottom: 'var(--space-1, 4px)',
};

const selectStyle = {
  width: '100%',
  padding: '8px 12px',
  borderRadius: 'var(--radius-md, 8px)',
  border: '1px solid var(--color-border, #DCDBDD)',
  background: 'var(--color-bg, #FFFFFF)',
  fontSize: '0.875rem',
  color: 'var(--color-text-primary, #111113)',
  cursor: 'pointer',
};

const inputStyle = {
  padding: '8px 12px',
  borderRadius: 'var(--radius-md, 8px)',
  border: '1px solid var(--color-border, #DCDBDD)',
  fontSize: '0.875rem',
};

const btnPrimary = {
  padding: '8px 16px',
  borderRadius: 'var(--radius-md, 8px)',
  border: '1px solid var(--color-primary-600, #276BF0)',
  background: 'var(--color-primary-600, #276BF0)',
  color: 'var(--color-text-inverse, #FFFFFF)',
  cursor: 'pointer',
  fontSize: '0.8125rem',
  fontWeight: '500',
};

const btnSecondary = {
  padding: '6px 12px',
  borderRadius: 'var(--radius-md, 8px)',
  border: '1px solid var(--color-border, #DCDBDD)',
  background: 'transparent',
  color: 'var(--color-text-secondary, #4F5056)',
  cursor: 'pointer',
  fontSize: '0.8125rem',
};

const btnDanger = {
  padding: '8px 12px',
  borderRadius: 'var(--radius-md, 8px)',
  border: '1px solid var(--color-border, #DCDBDD)',
  background: 'transparent',
  color: 'var(--color-danger, #9A3D4A)',
  cursor: 'pointer',
  fontSize: '0.8125rem',
};

const btnDangerSmall = {
  padding: '4px 12px',
  borderRadius: 'var(--radius-sm, 4px)',
  border: 'none',
  background: 'var(--color-danger, #9A3D4A)',
  color: '#fff',
  cursor: 'pointer',
  fontSize: '0.75rem',
  fontWeight: '500',
};

const btnSecondarySmall = {
  padding: '4px 12px',
  borderRadius: 'var(--radius-sm, 4px)',
  border: '1px solid var(--color-border, #DCDBDD)',
  background: 'transparent',
  color: 'var(--color-text-secondary, #4F5056)',
  cursor: 'pointer',
  fontSize: '0.75rem',
};

const linkBtn = {
  fontSize: '0.75rem',
  color: 'var(--color-primary-600, #276BF0)',
  background: 'none',
  border: 'none',
  cursor: 'pointer',
  fontWeight: '500',
  padding: '2px 4px',
};

const confirmBox = {
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
};

function FilterSelect({ label, value, onChange, options }) {
  return (
    <div>
      <label style={labelStyle}>{label}</label>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        style={{ ...selectStyle, minWidth: '140px' }}
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
      <label style={labelStyle}>{label}</label>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        min={min}
        style={{ ...inputStyle, width: type === 'date' ? '150px' : '120px' }}
      />
    </div>
  );
}
