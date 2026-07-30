import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useCoursesQuery } from '@/features/courses'
import {
  useAbsenceDashboardQuery,
  useDashboardViewsQuery,
  useDeleteDashboardViewMutation,
  useSaveDashboardViewMutation,
  useTouchDashboardViewMutation,
} from '../api/absence.queries'
import type { DashboardFilters, DashboardView } from '../api/absence.schemas'
import {
  filtersFromSearchParams,
  filtersToSearchParams,
} from '../lib/filter-params'
import { AbsenceResults } from '../components/AbsenceResults'
import { DashboardFiltersPanel } from '../components/DashboardFilters'
import { AsyncPage } from '@/shared/ui/AsyncPage'
import { Button } from '@/shared/ui/Button'
import { Dialog } from '@/shared/ui/Dialog'
import { EmptyState } from '@/shared/ui/EmptyState'
import { Field } from '@/shared/ui/Field'
import { Input } from '@/shared/ui/Input'
import { PageHeader } from '@/shared/ui/PageHeader'
import { Select } from '@/shared/ui/Select'
import { getErrorMessage } from '@/shared/lib/errors'
import { useToast } from '@/shared/ui/Toast'
import '../absence-dashboard.css'

export function Component() {
  const [params, setParams] = useSearchParams()
  const filters = useMemo(() => filtersFromSearchParams(params), [params])
  const enabled = params.get('load') === '1'
  const [activeView, setActiveView] = useState<DashboardView>()
  const [saveDialogOpen, setSaveDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [viewName, setViewName] = useState('')
  const coursesQuery = useCoursesQuery()
  const reportQuery = useAbsenceDashboardQuery(filters, enabled)
  const viewsQuery = useDashboardViewsQuery()
  const saveView = useSaveDashboardViewMutation()
  const deleteView = useDeleteDashboardViewMutation()
  const touchView = useTouchDashboardViewMutation()
  const { announce } = useToast()
  const views = viewsQuery.data ?? []

  const setFilters = (nextFilters: DashboardFilters) => {
    const next = filtersToSearchParams(nextFilters)
    if (enabled) {
      next.set('load', '1')
    }
    setParams(next, { replace: true })
  }
  const loadReport = () => {
    const next = filtersToSearchParams(filters)
    next.set('load', '1')
    setParams(next, { replace: true })
    if (enabled) {
      void reportQuery.refetch()
    }
  }
  const loadView = (view: DashboardView) => {
    setActiveView(view)
    setViewName(view.name)
    const next = filtersToSearchParams(view.filters)
    next.set('load', '1')
    setParams(next)
    touchView.mutate(view.id)
  }
  const handleSave = () => {
    const name = viewName.trim()
    if (name.length === 0) {
      return
    }
    saveView.mutate(
      activeView === undefined ? {
        name,
        filters,
      } : {
        id: activeView.id,
        name,
        filters,
      },
      {
        onSuccess: (saved) => {
          setActiveView(saved)
          setSaveDialogOpen(false)
          announce('Dashboard view saved.', 'success')
        },
        onError: (error) => announce(getErrorMessage(error), 'error'),
      },
    )
  }

  return (
    <section>
      <PageHeader
        eyebrow="Absence alerts"
        title="Student absence dashboard"
        description="Compare attendance across courses, focus on at-risk students, and save reusable views."
        actions={
          <>
            <Select
              aria-label="Saved dashboard view"
              value={activeView?.id ?? ''}
              onChange={(event) => {
                const id = Number(event.target.value)
                const selected = views.find((view) => view.id === id)
                if (selected !== undefined) {
                  loadView(selected)
                }
              }}
            >
              <option value="">Saved views</option>
              {views.map((view) => <option key={view.id} value={view.id}>{view.name}</option>)}
            </Select>
            <Button
              onClick={() => {
                setViewName(activeView?.name ?? '')
                setSaveDialogOpen(true)
              }}
            >
              {activeView === undefined ? 'Save view' : 'Update view'}
            </Button>
            {activeView !== undefined && (
              <Button variant="danger" onClick={() => setDeleteDialogOpen(true)}>Delete view</Button>
            )}
          </>
        }
      />
      <DashboardFiltersPanel
        courses={coursesQuery.data?.courses ?? []}
        filters={filters}
        loading={reportQuery.isFetching}
        onChange={setFilters}
        onLoad={loadReport}
      />
      {!enabled ? (
        <EmptyState
          title="Configure your dashboard"
          description="Choose courses and filters, then load the dashboard to compute attendance."
        />
      ) : (
        <AsyncPage
          pending={reportQuery.isPending}
          fetching={reportQuery.isFetching}
          error={reportQuery.error === null ? null : getErrorMessage(reportQuery.error)}
          empty={reportQuery.data?.students.length === 0}
          emptyTitle="No absences in this view"
          emptyDescription="All matching students are above the selected threshold."
          onRetry={() => void reportQuery.refetch()}
        >
          {reportQuery.data !== undefined && <AbsenceResults report={reportQuery.data} />}
        </AsyncPage>
      )}
      <Dialog
        description="Save the current URL-backed filters for quick access."
        onClose={() => setSaveDialogOpen(false)}
        open={saveDialogOpen}
        title={activeView === undefined ? 'Save dashboard view' : 'Update dashboard view'}
      >
        <div className="stack">
          <Field label="View name" required>
            {(fieldProps) => (
              <Input
                {...fieldProps}
                autoFocus
                value={viewName}
                onChange={(event) => setViewName(event.target.value)}
              />
            )}
          </Field>
          <div className="cluster">
            <Button
              disabled={viewName.trim().length === 0}
              loading={saveView.isPending}
              variant="primary"
              onClick={handleSave}
            >
              Save
            </Button>
            <Button variant="ghost" onClick={() => setSaveDialogOpen(false)}>Cancel</Button>
          </div>
        </div>
      </Dialog>
      <Dialog
        description={`This permanently deletes “${activeView?.name ?? 'this view'}”.`}
        onClose={() => setDeleteDialogOpen(false)}
        open={deleteDialogOpen}
        title="Delete saved view?"
      >
        <div className="cluster">
          <Button
            loading={deleteView.isPending}
            variant="danger"
            onClick={() => {
              if (activeView === undefined) {
                return
              }
              deleteView.mutate(activeView.id, {
                onSuccess: () => {
                  setActiveView(undefined)
                  setDeleteDialogOpen(false)
                  announce('Dashboard view deleted.', 'success')
                },
                onError: (error) => announce(getErrorMessage(error), 'error'),
              })
            }}
          >
            Delete view
          </Button>
          <Button variant="ghost" onClick={() => setDeleteDialogOpen(false)}>Cancel</Button>
        </div>
      </Dialog>
    </section>
  )
}
