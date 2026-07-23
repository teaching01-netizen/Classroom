import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchFresh } from '../api/fetchFresh';

describe('fetchFresh', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('forces browser cache mode to no-store', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}'));

    await fetchFresh('/api/teacher/courses');

    expect(fetchMock).toHaveBeenCalledWith('/api/teacher/courses', { cache: 'no-store' });
  });

  it('overrides a caller cache mode while preserving request options', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}'));
    const signal = { aborted: false };
    const headers = { 'Content-Type': 'application/json' };

    await fetchFresh('/api/teacher/courses', {
      method: 'POST',
      body: '{}',
      signal,
      headers,
      cache: 'force-cache',
    });

    expect(fetchMock).toHaveBeenCalledWith('/api/teacher/courses', {
      method: 'POST',
      body: '{}',
      signal,
      headers,
      cache: 'no-store',
    });
  });
});
