import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { FilterBar } from '../components/dashboard/FilterBar';
import { useDashboardFiltersStore } from '../store/useDashboardFiltersStore';

describe('FilterBar WCode input', () => {
  beforeEach(() => {
    useDashboardFiltersStore.getState().resetFilters();
  });

  it('renders WCode textarea', () => {
    render(
      <FilterBar
        courses={[]}
        coursesLoading={false}
        views={[]}
        activeViewId={null}
        onLoadView={() => {}}
        onSaveView={() => {}}
        onDeleteView={() => {}}
        onLoadDashboard={() => {}}
        dashboardLoading={false}
      />,
    );
    expect(screen.getByPlaceholderText(/wcode/i)).toBeTruthy();
  });

  it('parses comma-separated WCodes on input', () => {
    render(
      <FilterBar
        courses={[]}
        coursesLoading={false}
        views={[]}
        activeViewId={null}
        onLoadView={() => {}}
        onSaveView={() => {}}
        onDeleteView={() => {}}
        onLoadDashboard={() => {}}
        dashboardLoading={false}
      />,
    );
    const textarea = screen.getByPlaceholderText(/wcode/i);
    fireEvent.change(textarea, { target: { value: 'W12345, W67890' } });
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.wCodes).toEqual(['W12345', 'W67890']);
  });

  it('parses newline-separated WCodes on input', () => {
    render(
      <FilterBar
        courses={[]}
        coursesLoading={false}
        views={[]}
        activeViewId={null}
        onLoadView={() => {}}
        onSaveView={() => {}}
        onDeleteView={() => {}}
        onLoadDashboard={() => {}}
        dashboardLoading={false}
      />,
    );
    const textarea = screen.getByPlaceholderText(/wcode/i);
    fireEvent.change(textarea, { target: { value: 'W12345\nW67890\nW11111' } });
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.wCodes).toEqual(['W12345', 'W67890', 'W11111']);
  });

  it('parses space-separated WCodes on input', () => {
    render(
      <FilterBar
        courses={[]}
        coursesLoading={false}
        views={[]}
        activeViewId={null}
        onLoadView={() => {}}
        onSaveView={() => {}}
        onDeleteView={() => {}}
        onLoadDashboard={() => {}}
        dashboardLoading={false}
      />,
    );
    const textarea = screen.getByPlaceholderText(/wcode/i);
    fireEvent.change(textarea, { target: { value: 'W12345 W67890' } });
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.wCodes).toEqual(['W12345', 'W67890']);
  });

  it('trims whitespace from WCodes', () => {
    render(
      <FilterBar
        courses={[]}
        coursesLoading={false}
        views={[]}
        activeViewId={null}
        onLoadView={() => {}}
        onSaveView={() => {}}
        onDeleteView={() => {}}
        onLoadDashboard={() => {}}
        dashboardLoading={false}
      />,
    );
    const textarea = screen.getByPlaceholderText(/wcode/i);
    fireEvent.change(textarea, { target: { value: '  W12345  ,  W67890  ' } });
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.wCodes).toEqual(['W12345', 'W67890']);
  });

  it('ignores empty entries from pasted text', () => {
    render(
      <FilterBar
        courses={[]}
        coursesLoading={false}
        views={[]}
        activeViewId={null}
        onLoadView={() => {}}
        onSaveView={() => {}}
        onDeleteView={() => {}}
        onLoadDashboard={() => {}}
        dashboardLoading={false}
      />,
    );
    const textarea = screen.getByPlaceholderText(/wcode/i);
    fireEvent.change(textarea, { target: { value: 'W12345,,,W67890' } });
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.wCodes).toEqual(['W12345', 'W67890']);
  });

  it('sets empty array when textarea is cleared', () => {
    render(
      <FilterBar
        courses={[]}
        coursesLoading={false}
        views={[]}
        activeViewId={null}
        onLoadView={() => {}}
        onSaveView={() => {}}
        onDeleteView={() => {}}
        onLoadDashboard={() => {}}
        dashboardLoading={false}
      />,
    );
    const textarea = screen.getByPlaceholderText(/wcode/i);
    fireEvent.change(textarea, { target: { value: 'W12345' } });
    fireEvent.change(textarea, { target: { value: '' } });
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.wCodes).toEqual([]);
  });
});
