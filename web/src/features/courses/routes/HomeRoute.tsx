import { Link } from 'react-router-dom'
import { useBatchAttendanceQuery } from '@/features/attendance'
import { useCoursesQuery, useFavouritesQuery } from '../api/course.queries'
import { useToggleFavouriteMutation } from '../api/course.mutations'
import { CourseCard } from '../components/CourseCard'
import { AsyncPage } from '@/shared/ui/AsyncPage'
import { EmptyState } from '@/shared/ui/EmptyState'
import { PageHeader } from '@/shared/ui/PageHeader'
import { getErrorMessage } from '@/shared/lib/errors'
import { useToast } from '@/shared/ui/Toast'
import '../courses.css'

export function Component() {
  const coursesQuery = useCoursesQuery()
  const favouritesQuery = useFavouritesQuery()
  const toggleFavourite = useToggleFavouriteMutation()
  const { announce } = useToast()
  const favouriteIds = favouritesQuery.data?.favourite_ids ?? []
  const pinnedCourses = (coursesQuery.data?.courses ?? []).filter((course) =>
    favouriteIds.includes(course.course_id),
  )
  const attendanceQuery = useBatchAttendanceQuery(
    pinnedCourses.map((course) => course.course_id),
  )
  const error = coursesQuery.error ?? favouritesQuery.error

  return (
    <section>
      <PageHeader
        eyebrow="Overview"
        title="Your teaching dashboard"
        description="Pinned courses, live attendance, and the sessions that need attention."
      />
      <AsyncPage
        pending={coursesQuery.isPending || favouritesQuery.isPending}
        fetching={coursesQuery.isFetching || favouritesQuery.isFetching}
        error={error === null ? null : getErrorMessage(error)}
        empty={false}
        emptyTitle=""
        emptyDescription=""
        onRetry={() => {
          void coursesQuery.refetch()
          void favouritesQuery.refetch()
        }}
      >
        {pinnedCourses.length === 0 ? (
          <EmptyState
            title="No pinned courses yet"
            description="Pin the courses you use most to see their live attendance here."
          >
            <Link className="button-link" to="/courses">Browse all courses</Link>
          </EmptyState>
        ) : (
          <div className="course-grid">
            {pinnedCourses.map((course) => (
              <CourseCard
                attendance={attendanceQuery.data?.courses[course.course_id]}
                attendancePending={attendanceQuery.isPending}
                course={course}
                key={course.course_id}
                pinned
                onTogglePinned={(pinned) => {
                  toggleFavourite.mutate(
                    { courseId: course.course_id, pinned },
                    {
                      onError: (mutationError) =>
                        announce(getErrorMessage(mutationError), 'error'),
                    },
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
