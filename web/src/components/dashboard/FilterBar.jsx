import React, { useState, useMemo, useEffect } from 'react';
import { useDashboardFiltersStore } from '../../store/useDashboardFiltersStore';

function parseWCodes(input) {
  if (!input.trim()) return [];
  const parts = input.split(/[, \n]+/);
  return parts.map((p) => p.trim()).filter(Boolean);
}

export function FilterBar({ courses, coursesLoading, activeViewId, onLoadView, onSaveView, onDeleteView, onLoadDashboard, dashboardLoading, views }) {
  const filters = useDashboardFiltersStore((s) => s.filters);
  const setCourseIds = useDashboardFiltersStore((s) => s.setCourseIds);
  const setThreshold = useDashboardFiltersStore((s) => s.setThreshold);
  const setSortBy = useDashboardFiltersStore((s) => s.setSortBy);
  const setDateRange = useDashboardFiltersStore((s) => s.setDateRange);
  const setWCodes = useDashboardFiltersStore((s) => s.setWCodes);
  const resetFilters = useDashboardFiltersStore((s) => s.resetFilters);

  const [showSaveDialog, setShowSaveDialog] = useState(false);
  const [viewName, setViewName] = useState('');
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(null);
  const [courseSearch, setCourseSearch] = useState('');
  const [showCoursePicker, setShowCoursePicker] = useState(false);
  const [wCodeInput, setWCodeInput] = useState((filters.wCodes || []).join(', '));

  useEffect(() => {
    const currentParsed = JSON.stringify(parseWCodes(wCodeInput));
    const storeParsed = JSON.stringify(filters.wCodes || []);
    if (currentParsed !== storeParsed) {
      setWCodeInput((filters.wCodes || []).join(', '));
    }
  }, [filters.wCodes]);

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

  const handleWCodeChange = (e) => {
    const raw = e.target.value;
    setWCodeInput(raw);
    setWCodes(parseWCodes(raw));
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
      borderRadius: 'var(--radius-md, 8px)',
      padding: 'var(--space-3, 12px) var(--space-4, 16px)',
      marginBottom: 'var(--space-4, 16px)',
    }}>
      {/* Saved Views Row */}
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--space-2, 8px)',
        marginBottom: 'var(--space-2, 8px)',
        flexWrap: 'wrap',
      }}>
        <div style={{ flex: 1, minWidth: '180px' }}>
          <select
            value={activeViewId || ''}
            onChange={(e) => {
              const id = e.target.value ? Number(e.target.value) : null;
              const view = (views || []).find((v) => v.id === id);
              if (view) onLoadView(view);
            }}
            style={selectStyle}
            aria-label="Saved Views"
          >
            <option value="">— Saved Views —</option>
            {(views || []).map((view) => (
              <option key={view.id} value={view.id}>{view.name}</option>
            ))}
          </select>
        </div>

        <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
          {!showSaveDialog ? (
            <button onClick={() => setShowSaveDialog(true)} style={btnPrimary}>
              Save
            </button>
          ) : (
            <div style={{ display: 'flex', gap: '6px' }}>
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
                style={{ ...inputStyle, width: '140px' }}
              />
              <button onClick={handleSave} disabled={!viewName.trim()} style={btnPrimary}>Save</button>
              <button onClick={() => setShowSaveDialog(false)} style={btnSecondary}>Cancel</button>
            </div>
          )}

          {activeViewId && (
            <div style={{ position: 'relative' }}>
              <button onClick={() => setShowDeleteConfirm(activeViewId)} style={btnDanger}>×</button>
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
      <div style={{ marginBottom: 'var(--space-2, 8px)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <button
            onClick={() => setShowCoursePicker(!showCoursePicker)}
            style={{
              fontSize: '0.75rem',
              color: 'var(--color-text-secondary, #4F5056)',
              background: 'none',
              border: 'none',
              cursor: 'pointer',
              fontWeight: '500',
              padding: 0,
            }}
          >
            Courses {showCoursePicker ? '▾' : '▸'}
          </button>
          {selectedCount > 0 && (
            <span style={{
              fontSize: '0.6875rem',
              fontWeight: '500',
              color: 'var(--color-primary-600, #276BF0)',
              background: 'color-mix(in srgb, var(--color-primary-600, #276BF0) 10%, transparent)',
              padding: '1px 6px',
              borderRadius: 'var(--radius-sm, 4px)',
            }}>
              {selectedCount}
            </span>
          )}
        </div>

        {showCoursePicker && (
          <div style={{
            border: '1px solid var(--color-border, #DCDBDD)',
            borderRadius: 'var(--radius-sm, 4px)',
            overflow: 'hidden',
            marginTop: '6px',
          }}>
            <div style={{
              padding: '4px 8px',
              background: 'var(--color-bg-subtle, #F5F5F5)',
              borderBottom: '1px solid var(--color-border, #DCDBDD)',
              display: 'flex',
              gap: '6px',
              alignItems: 'center',
            }}>
              <input
                type="text"
                value={courseSearch}
                onChange={(e) => setCourseSearch(e.target.value)}
                placeholder="Search courses..."
                style={{ ...inputStyle, flex: 1, margin: 0, padding: '4px 8px', fontSize: '0.75rem' }}
              />
              <button onClick={selectAll} style={linkBtn}>All</button>
              <button onClick={clearAll} style={linkBtn}>None</button>
            </div>

            <div style={{ maxHeight: '180px', overflowY: 'auto' }}>
              {coursesLoading ? (
                <div style={{ padding: '12px', textAlign: 'center', color: 'var(--color-text-muted, #696A6C)', fontSize: '0.75rem' }}>
                  Loading courses...
                </div>
              ) : filteredCourses.length === 0 ? (
                <div style={{ padding: '12px', textAlign: 'center', color: 'var(--color-text-muted, #696A6C)', fontSize: '0.75rem' }}>
                  No courses found
                </div>
              ) : (
                filteredCourses.map((course) => (
                  <label
                    key={course.course_id}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '8px',
                      padding: '5px 8px',
                      borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
                      cursor: 'pointer',
                      fontSize: '0.75rem',
                      background: (filters.courseIds || []).includes(course.course_id)
                        ? 'color-mix(in srgb, var(--color-primary-600, #276BF0) 5%, transparent)'
                        : 'transparent',
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={(filters.courseIds || []).includes(course.course_id)}
                      onChange={() => toggleCourse(course.course_id)}
                      style={{ accentColor: 'var(--color-primary-600, #276BF0)', width: '14px', height: '14px' }}
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
                    </div>
                  </label>
                ))
              )}
            </div>
          </div>
        )}
      </div>

      {/* WCode Filter */}
      <div style={{ marginBottom: 'var(--space-2, 8px)' }}>
        <textarea
          value={wCodeInput}
          onChange={handleWCodeChange}
          placeholder="Paste WCode(s) — comma, space, or line separated"
          rows={2}
          style={{
            ...inputStyle,
            width: '100%',
            resize: 'vertical',
            fontFamily: 'monospace',
            fontSize: '0.75rem',
            lineHeight: '1.4',
          }}
        />
        {filters.wCodes.length > 0 && (
          <span style={{
            fontSize: '0.6875rem',
            color: 'var(--color-text-muted, #696A6C)',
            marginTop: '2px',
            display: 'block',
          }}>
            {filters.wCodes.length} WCode{filters.wCodes.length !== 1 ? 's' : ''} applied
          </span>
        )}
      </div>

      {/* Filter Row */}
      <div style={{
        display: 'flex',
        gap: 'var(--space-2, 8px)',
        flexWrap: 'wrap',
        alignItems: 'center',
        marginBottom: 'var(--space-2, 8px)',
      }}>
        <FilterSelect
          value={filters.sortBy}
          onChange={setSortBy}
          options={[
            { value: 'risk', label: 'Sort: at risk' },
            { value: 'rate-asc', label: 'Sort: attendance ↑' },
            { value: 'rate-desc', label: 'Sort: attendance ↓' },
            { value: 'name', label: 'Sort: name' },
          ]}
        />

        <FilterInput
          type="number"
          value={filters.threshold || ''}
          onChange={(v) => setThreshold(Number(v) || 0)}
          placeholder="≥ absences"
          min="0"
          ariaLabel="Absence threshold"
        />

        <FilterInput
          type="date"
          value={filters.dateRange?.from || ''}
          onChange={(v) => {
            const to = filters.dateRange?.to || '';
            setDateRange(v || to ? { from: v, to } : null);
          }}
          ariaLabel="From date"
        />

        <FilterInput
          type="date"
          value={filters.dateRange?.to || ''}
          onChange={(v) => {
            const from = filters.dateRange?.from || '';
            setDateRange(from || v ? { from, to: v } : null);
          }}
          ariaLabel="To date"
        />

        <button onClick={() => { resetFilters(); setWCodeInput(''); }} style={linkBtn}>Reset</button>
      </div>

      {/* Load Button */}
      <div style={{ display: 'flex', alignItems: 'center' }}>
        <button
          onClick={onLoadDashboard}
          disabled={dashboardLoading}
          style={{
            padding: '6px 14px',
            borderRadius: 'var(--radius-sm, 4px)',
            border: 'none',
            background: dashboardLoading ? 'var(--color-bg-hover, #F1F2F4)' : 'var(--color-primary-600, #276BF0)',
            color: dashboardLoading ? 'var(--color-text-muted, #696A6C)' : 'var(--color-text-inverse, #FFFFFF)',
            cursor: dashboardLoading ? 'not-allowed' : 'pointer',
            fontWeight: '500',
            fontSize: '0.8125rem',
            transition: 'background 0.2s',
          }}
        >
          {dashboardLoading ? 'Loading...' : 'Load Dashboard'}
        </button>
      </div>
    </div>
  );
}

const selectStyle = {
  width: '100%',
  padding: '5px 8px',
  borderRadius: 'var(--radius-sm, 4px)',
  border: '1px solid var(--color-border, #DCDBDD)',
  background: 'var(--color-bg, #FFFFFF)',
  fontSize: '0.8125rem',
  color: 'var(--color-text-primary, #111113)',
  cursor: 'pointer',
};

const inputStyle = {
  padding: '5px 8px',
  borderRadius: 'var(--radius-sm, 4px)',
  border: '1px solid var(--color-border, #DCDBDD)',
  fontSize: '0.8125rem',
};

const btnPrimary = {
  padding: '5px 10px',
  borderRadius: 'var(--radius-sm, 4px)',
  border: '1px solid var(--color-primary-600, #276BF0)',
  background: 'var(--color-primary-600, #276BF0)',
  color: 'var(--color-text-inverse, #FFFFFF)',
  cursor: 'pointer',
  fontSize: '0.75rem',
  fontWeight: '500',
};

const btnSecondary = {
  padding: '5px 10px',
  borderRadius: 'var(--radius-sm, 4px)',
  border: '1px solid var(--color-border, #DCDBDD)',
  background: 'transparent',
  color: 'var(--color-text-secondary, #4F5056)',
  cursor: 'pointer',
  fontSize: '0.75rem',
};

const btnDanger = {
  padding: '2px 8px',
  borderRadius: 'var(--radius-sm, 4px)',
  border: '1px solid var(--color-border, #DCDBDD)',
  background: 'transparent',
  color: 'var(--color-danger, #9A3D4A)',
  cursor: 'pointer',
  fontSize: '0.875rem',
  fontWeight: '600',
  lineHeight: 1,
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

function FilterSelect({ value, onChange, options }) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      style={{ ...selectStyle, minWidth: '150px' }}
    >
      {options.map((opt) => (
        <option key={opt.value} value={opt.value}>{opt.label}</option>
      ))}
    </select>
  );
}

function FilterInput({ type, value, onChange, placeholder, min, ariaLabel }) {
  return (
    <input
      type={type}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      min={min}
      aria-label={ariaLabel}
      style={{ ...inputStyle, width: type === 'date' ? '130px' : '100px' }}
    />
  );
}
