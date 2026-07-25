import fs from 'node:fs';
import { pathToFileURL } from 'node:url';

function finiteNumber(value, fallback = 0) {
  return Number.isFinite(value) ? value : fallback;
}

export function rolloutSummary(response) {
  const data = response?.data ?? {};
  const currentByKind = data.current_by_kind ?? {};
  const activeCourseTargets = finiteNumber(data.active_course_targets);
  const activeCourseCurrent = finiteNumber(data.active_course_current);
  const knownSessionTargets = finiteNumber(data.known_session_targets);
  const knownSessionCurrent = finiteNumber(data.known_session_current);
  return {
    catalog_current: finiteNumber(currentByKind.course_catalog),
    profiles_current: finiteNumber(currentByKind.student_profiles),
    active_course_current: activeCourseCurrent,
    active_course_targets: activeCourseTargets,
    active_course_coverage: `${activeCourseCurrent}/${activeCourseTargets}`,
    known_session_current: knownSessionCurrent,
    known_session_targets: knownSessionTargets,
    known_session_coverage: `${knownSessionCurrent}/${knownSessionTargets}`,
    expired_current: finiteNumber(data.expired_current, -1),
    active_permits: finiteNumber(data.active_permits, -1),
    host_paused_until: data.host_paused_until ?? null,
  };
}

export function isRolloutReady(response) {
  const summary = rolloutSummary(response);
  return response?.success === true
    && summary.catalog_current === 1
    && summary.profiles_current === 1
    && summary.active_course_targets > 0
    && summary.active_course_current === summary.active_course_targets
    && summary.known_session_targets > 0
    && summary.known_session_current === summary.known_session_targets
    && summary.expired_current === 0
    && summary.active_permits === 0
    && summary.host_paused_until === null;
}

function main() {
  if (process.argv.length !== 3) {
    console.error('usage: node check-snapshot-rollout-readiness.mjs status-response.json');
    process.exitCode = 2;
    return;
  }
  let response;
  try {
    response = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
  } catch {
    console.error('snapshot status response is not valid JSON');
    process.exitCode = 2;
    return;
  }
  const summary = rolloutSummary(response);
  const output = JSON.stringify(summary, null, 2);
  if (!isRolloutReady(response)) {
    console.error('snapshot rollout is not ready for read cutover');
    console.error(output);
    process.exitCode = 1;
    return;
  }
  console.log(output);
  console.log('snapshot rollout readiness gates passed');
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
