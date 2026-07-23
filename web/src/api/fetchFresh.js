export function fetchFresh(input, init = {}) {
  return fetch(input, {
    ...init,
    cache: 'no-store',
  });
}
