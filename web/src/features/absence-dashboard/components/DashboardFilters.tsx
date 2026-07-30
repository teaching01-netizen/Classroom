import type { CourseSummary } from '@/features/courses'
import { dashboardSortSchema, type DashboardFilters } from '../api/absence.schemas'
import { toggleCourseFilter } from '../lib/filter-params'
import { Button } from '@/shared/ui/Button'
import { Checkbox } from '@/shared/ui/Checkbox'
import { Field } from '@/shared/ui/Field'
import { Input } from '@/shared/ui/Input'
import { Select } from '@/shared/ui/Select'

type DashboardFiltersProps = {
  readonly courses: readonly CourseSummary[]
  readonly filters: DashboardFilters
  readonly loading: boolean
  readonly onChange: (filters: DashboardFilters) => void
  readonly onLoad: () => void
}

export function DashboardFiltersPanel({
  courses,
  filters,
  loading,
  onChange,
  onLoad,
}: DashboardFiltersProps) {
  return (
    <section className="dashboard-filters" aria-labelledby="dashboard-filters-title">
      <div className="dashboard-filters__header">
        <div>
          <h3 id="dashboard-filters-title">Report filters</h3>
          <p>Filter settings are saved in the URL and can be shared.</p>
        </div>
        <Button loading={loading} variant="primary" onClick={onLoad}>Load dashboard</Button>
      </div>
      <div className="dashboard-filters__grid">
        <fieldset className="course-picker">
          <legend>Courses</legend>
          <div>
            {courses.map((course) => (
              <Checkbox
                checked={filters.courseIds.includes(course.course_id)}
                key={course.course_id}
                label={course.name}
                onChange={() => onChange(toggleCourseFilter(filters, course.course_id))}
              />
            ))}
          </div>
        </fieldset>
        <Field label="Absence threshold" description="Minimum number of absences before a student is highlighted.">
          {(fieldProps) => (
            <Input
              {...fieldProps}
              min={0}
              type="number"
              value={filters.threshold}
              onChange={(event) => onChange({
                ...filters,
                threshold: Math.max(0, Number(event.target.value) || 0),
              })}
            />
          )}
        </Field>
        <Field label="Sort students">
          {(fieldProps) => (
            <Select
              {...fieldProps}
              value={filters.sortBy}
              onChange={(event) => {
                const result = dashboardSortSchema.safeParse(event.target.value)
                if (result.success) {
                  onChange({ ...filters, sortBy: result.data })
                }
              }}
            >
              <option value="risk">Risk first</option>
              <option value="rate-asc">Attendance, low to high</option>
              <option value="rate-desc">Attendance, high to low</option>
              <option value="name">Name</option>
            </Select>
          )}
        </Field>
        <Field label="WCodes" description="Optional comma- or newline-separated student IDs.">
          {(fieldProps) => (
            <textarea
              {...fieldProps}
              className="ui-input dashboard-filters__wcodes"
              value={filters.wCodes.join('\n')}
              onChange={(event) => onChange({
                ...filters,
                wCodes: event.target.value
                  .split(/[\s,]+/)
                  .map((value) => value.trim())
                  .filter(Boolean),
              })}
            />
          )}
        </Field>
      </div>
    </section>
  )
}
