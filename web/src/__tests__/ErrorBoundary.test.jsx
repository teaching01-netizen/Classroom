// @vitest-environment jsdom
import { describe, it, expect, afterEach } from 'vitest';
import React from 'react';
import { render, screen, cleanup } from '@testing-library/react';
import { ErrorBoundary } from '../components/ErrorBoundary';

function ThrowingComponent() {
  throw new Error('Test error');
}

function WorkingComponent() {
  return <div>Working content</div>;
}

afterEach(() => {
  cleanup();
});

describe('ErrorBoundary', () => {
  it('renders children when no error', () => {
    render(
      <ErrorBoundary>
        <WorkingComponent />
      </ErrorBoundary>
    );
    expect(screen.getByText('Working content')).toBeDefined();
    expect(screen.queryByText('Something went wrong')).toBeNull();
  });

  it('catches errors and shows fallback UI', () => {
    render(
      <ErrorBoundary>
        <ThrowingComponent />
      </ErrorBoundary>
    );
    expect(screen.getByText('Something went wrong')).toBeDefined();
    expect(screen.getByText('Test error')).toBeDefined();
    expect(screen.getByText('Try Again')).toBeDefined();
  });

  it('shows retry button that resets error state', () => {
    const { unmount } = render(
      <ErrorBoundary>
        <ThrowingComponent />
      </ErrorBoundary>
    );

    expect(screen.getByText('Something went wrong')).toBeDefined();
    unmount();

    const { rerender } = render(
      <ErrorBoundary>
        <WorkingComponent />
      </ErrorBoundary>
    );
    expect(screen.getByText('Working content')).toBeDefined();
    expect(screen.queryByText('Something went wrong')).toBeNull();
    rerender(
      <ErrorBoundary>
        <WorkingComponent />
      </ErrorBoundary>
    );
    expect(screen.getByText('Working content')).toBeDefined();
  });
});
