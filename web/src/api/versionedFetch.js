const REPLICA_LAG_RETRY_MS = 200;
const REPLICA_LAG_MAX_RETRIES = 2;

/**
 * Fetch with version-aware response rejection.
 * Reads X-Snapshot-Version from response headers and compares against
 * the caller's displayedVersion and requestedMinimumVersion. Returns null
 * if the response is stale (caller should discard).
 *
 * @param {string} url
 * @param {object} init - fetch options
 * @param {{ displayedVersion: number, requestedMinimumVersion: number }} versions
 * @returns {Promise<{ response: Response, retryCount: number } | null>}
 */
export async function fetchVersioned(url, init, versions) {
  let _lastVersion = 0;

  for (let attempt = 0; attempt <= REPLICA_LAG_MAX_RETRIES; attempt++) {
    const response = await fetch(url, { ...init, cache: 'no-store' });
    const versionHeader = response.headers.get('X-Snapshot-Version');
    const responseVersion = versionHeader ? parseInt(versionHeader, 10) : NaN;

    if (!isNaN(responseVersion)) {
      // Reject if version is older than what we've already displayed
      if (versions.displayedVersion > 0 && responseVersion < versions.displayedVersion) {
        return null;
      }
      // Reject if version is below the minimum we expect
      if (versions.requestedMinimumVersion > 0 && responseVersion < versions.requestedMinimumVersion) {
        // Retry briefly for read replica lag
        if (attempt < REPLICA_LAG_MAX_RETRIES) {
          await new Promise(resolve => setTimeout(resolve, REPLICA_LAG_RETRY_MS));
          _lastVersion = responseVersion;
          continue;
        }
        return null;
      }
      _lastVersion = responseVersion;
    }

    return { response, retryCount: attempt };
  }

  return null;
}
