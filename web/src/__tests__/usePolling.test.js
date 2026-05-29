// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { usePolling } from '../hooks/usePolling';

describe('usePolling', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('calls the callback at the specified interval', () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => usePolling(cb, 1000));

    vi.advanceTimersByTime(1000);
    expect(cb).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(1000);
    expect(cb).toHaveBeenCalledTimes(2);

    vi.advanceTimersByTime(1000);
    expect(cb).toHaveBeenCalledTimes(3);

    unmount();
  });

  it('does not call callback when enabled is false', () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => usePolling(cb, 1000, false));

    vi.advanceTimersByTime(3000);
    expect(cb).not.toHaveBeenCalled();

    unmount();
  });

  it('stops polling on unmount', () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => usePolling(cb, 1000));

    vi.advanceTimersByTime(1000);
    expect(cb).toHaveBeenCalledTimes(1);

    unmount();

    vi.advanceTimersByTime(3000);
    expect(cb).toHaveBeenCalledTimes(1);
  });

  it('does not start with 0 interval', () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => usePolling(cb, 0));

    vi.advanceTimersByTime(3000);
    expect(cb).not.toHaveBeenCalled();

    unmount();
  });

  it('does not start with negative interval', () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => usePolling(cb, -1000));

    vi.advanceTimersByTime(3000);
    expect(cb).not.toHaveBeenCalled();

    unmount();
  });

  it('executes immediately when immediate flag is true', () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => usePolling(cb, 1000, true, true));

    expect(cb).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(1000);
    expect(cb).toHaveBeenCalledTimes(2);

    unmount();
  });

  it('updates callback ref across re-renders', () => {
    const cb1 = vi.fn();
    const cb2 = vi.fn();

    const { rerender, unmount } = renderHook(
      ({ callback }) => usePolling(callback, 1000),
      { initialProps: { callback: cb1 } }
    );

    vi.advanceTimersByTime(1000);
    expect(cb1).toHaveBeenCalledTimes(1);

    rerender({ callback: cb2 });
    vi.advanceTimersByTime(1000);
    expect(cb2).toHaveBeenCalledTimes(1);
    expect(cb1).toHaveBeenCalledTimes(1);

    unmount();
  });
});
