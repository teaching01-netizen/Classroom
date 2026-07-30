import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { CourseCard } from '../components/CourseCard'
import { useCoursesQuery, useFavouritesQuery } from '../api/course.queries'
import { useToggleFavouriteMutation } from '../api/course.mutations'
import { AsyncPage } from '@/shared/ui/AsyncPage'
import { Field } from '@/shared/ui/Field'
import { Input } from '@/shared/ui/Input'
import { Select } from '@/shared/ui/Select'
import { PageHeader } from '@/shared/ui/PageHeader'
import { StatsGrid } from '@/shared/ui/StatsGrid'
import { getErrorMessage } from '@/shared/lib/errors'
import { useToast } from '@/shared/ui/Toast'
import type { CourseSummary } from '../api/course.schemas'
import '../courses.css'

const validStatuses = ['all', 'active', 'upcoming', 'finished'] as const
type StatusFilter = (typeof validStatuses)[number]

function isStatusFilter(value: string | null): value is StatusFilter {
  return validStatuses.some((status) => status === value)
}

export function Component() {
  const [params, setParams] = useSearchParams()
  const coursesQuery = useCoursesQuery()
  const favouritesQuery = useFavouritesQuery()
  const toggleFavourite = useToggleFavouriteMutation()
  const { announce } = useToast()
  const search = params.get('search') ?? ''
  const rawStatus = params.get('status')
  const status: StatusFilter = isStatusFilter(rawStatus) ? rawStatus : 'all'
  const courses = useMemo(() => coursesQuery.data?.courses ?? [], [coursesQuery.data])
  const favouriteIds = favouritesQuery.data?.favourite_ids ?? []

  const filteredCourses = useMemo(
    () => courses.filter((course) => {
      const matchesSearch = course.name.toLowerCase().includes(search.toLowerCase())
      const matchesStatus = status === 'all' || course.status === status
      return matchesSearch && matchesStatus
    }),
    [courses, search, status],
  )
  const stats = useMemo(
    () => [
      { label: 'Total courses', value: courses.length },
      { label: 'Active', value: courses.filter((course) => course.status === 'active').length },
      { label: 'Students', value: courses.reduce((sum, course) => sum + course.enrolled_count, 0) },
      {
        label: 'Average attendance',
        value: courses.length === 0
          ? '—'
          : `${Math.round(courses.reduce((sum, course) => sum + course.avg_attendance_rate, 0) / courses.length * 100)}%`,
      },
    ] as const,
    [courses],
  )
  const updateParam = (key: string, value: string, emptyValue = '') => {
    const next = new URLSearchParams(params)
    if (value === emptyValue) {
      next.delete(key)
    } else {
      next.set(key, value)
    }
    setParams(next, { replace: true })
  }

  return (
    <section>
      <PageHeader
        eyebrow="Courses"
        title="All courses"
        description="Search, pin, and open the courses assigned to you."
      />
      <AsyncPage
        pending={coursesQuery.isPending || favouritesQuery.isPending}
        fetching={coursesQuery.isFetching || favouritesQuery.isFetching}
        error={
          coursesQuery.error !== null
            ? getErrorMessage(coursesQuery.error)
            : favouritesQuery.error !== null
              ? getErrorMessage(favouritesQuery.error)
              : null
        }
        empty={courses.length === 0}
        emptyTitle="No courses assigned"
        emptyDescription="Assigned courses will appear here when the catalog refreshes."
        onRetry={() => {
          void coursesQuery.refetch()
          void favouritesQuery.refetch()
        }}
      >
        <StatsGrid stats={stats} />
        <div className="filter-bar">
          <Field label="Search courses">
            {(fieldProps) => (
              <Input
                {...fieldProps}
                type="search"
                value={search}
                onChange={(event) => updateParam('search', event.target.value)}
              />
            )}
          </Field>
          <Field label="Course status">
            {(fieldProps) => (
              <Select
                {...fieldProps}
                value={status}
                onChange={(event) => updateParam('status', event.target.value, 'all')}
              >
                <option value="all">All statuses</option>
                <option value="active">Active</option>
                <option value="upcoming">Upcoming</option>
                <option value="finished">Finished</option>
              </Select>
            )}
          </Field>
        </div>
        {filteredCourses.length === 0 ? (
          <EmptySearch courses={courses} />
        ) : (
          <div className="course-grid">
            {filteredCourses.map((course) => (
              <CourseCard
                course={course}
                key={course.course_id}
                pinned={favouriteIds.includes(course.course_id)}
                onTogglePinned={(pinned) => {
                  toggleFavourite.mutate(
                    { courseId: course.course_id, pinned },
                    { onError: (error) => announce(getErrorMessage(error), 'error') },
                  )
                }}
              />
            ))}
          </div>
        )}
      </AsyncPage>
    </section>
  )
}

function EmptySearch({ courses }: { readonly courses: readonly CourseSummary[] }) {
  return courses.length === 0
    ? null
    : (
      <p className="no-results" role="status">
        No courses match these filters. Clear the search or choose another status.
      </p>
    )
}
