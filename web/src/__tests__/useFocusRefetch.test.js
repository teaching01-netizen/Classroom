// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useFocusRefetch } from '../hooks/useFocusRefetch';

describe('useFocusRefetch', () => {
  beforeEach(() => {
    vi.spyOn(document, 'addEventListener').mockImplementation(() => {});
    vi.spyOn(document, 'removeEventListener').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('registers a visibilitychange listener on mount', () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => useFocusRefetch(cb));
    expect(document.addEventListener).toHaveBeenCalledWith(
      'visibilitychange',
      expect.any(Function),
      false
    );
    unmount();
  });

  it('calls refetch callback when document becomes visible', () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => useFocusRefetch(cb));

    const handler = document.addEventListener.mock.calls.find(
      (c) => c[0] === 'visibilitychange'
    )[1];

    Object.defineProperty(document, 'visibilityState', {
      value: 'visible',
      configurable: true,
    });
    handler();

    expect(cb).toHaveBeenCalledTimes(1);
    unmount();
  });

  it('does not call refetch when document becomes hidden', () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => useFocusRefetch(cb));

    const handler = document.addEventListener.mock.calls.find(
      (c) => c[0] === 'visibilitychange'
    )[1];

    Object.defineProperty(document, 'visibilityState', {
      value: 'hidden',
      configurable: true,
    });
    handler();

    expect(cb).not.toHaveBeenCalled();
    unmount();
  });

  it('calls refetch multiple times on repeated focus', () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => useFocusRefetch(cb));

    const handler = document.addEventListener.mock.calls.find(
      (c) => c[0] === 'visibilitychange'
    )[1];

    Object.defineProperty(document, 'visibilityState', {
      value: 'visible',
      configurable: true,
    });
    handler();
    handler();

    expect(cb).toHaveBeenCalledTimes(2);
    unmount();
  });

  it('removes visibilitychange listener on unmount', () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => useFocusRefetch(cb));
    unmount();

    expect(document.removeEventListener).toHaveBeenCalledWith(
      'visibilitychange',
      expect.any(Function),
      false
    );
  });

  it('uses latest callback via ref across re-renders', () => {
    const cb1 = vi.fn();
    const cb2 = vi.fn();

    const { rerender, unmount } = renderHook(
      ({ callback }) => useFocusRefetch(callback),
      { initialProps: { callback: cb1 } }
    );

    const handler = document.addEventListener.mock.calls.find(
      (c) => c[0] === 'visibilitychange'
    )[1];

    Object.defineProperty(document, 'visibilityState', {
      value: 'visible',
      configurable: true,
    });
    handler();
    expect(cb1).toHaveBeenCalledTimes(1);

    rerender({ callback: cb2 });
    handler();
    expect(cb2).toHaveBeenCalledTimes(1);
    expect(cb1).toHaveBeenCalledTimes(1);

    unmount();
  });
});
