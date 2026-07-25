import assert from 'node:assert/strict';
import test from 'node:test';

import {
  isRolloutReady,
  rolloutSummary,
} from './check-snapshot-rollout-readiness.mjs';

function readyResponse() {
  return {
    success: true,
    data: {
      current_by_kind: {
        course_catalog: 1,
        student_profiles: 1,
      },
      active_course_targets: 2,
      active_course_current: 2,
      known_session_targets: 5,
      known_session_current: 5,
      expired_current: 0,
      active_permits: 0,
      host_paused_until: null,
    },
  };
}

test('accepts complete non-zero rollout coverage', () => {
  const response = readyResponse();
  assert.equal(isRolloutReady(response), true);
  assert.deepEqual(rolloutSummary(response), {
    catalog_current: 1,
    profiles_current: 1,
    active_course_current: 2,
    active_course_targets: 2,
    active_course_coverage: '2/2',
    known_session_current: 5,
    known_session_targets: 5,
    known_session_coverage: '5/5',
    expired_current: 0,
    active_permits: 0,
    host_paused_until: null,
  });
});

test('rejects zero denominators and every unhealthy cutover gate', () => {
  const mutations = [
    (data) => { data.current_by_kind.course_catalog = 0; },
    (data) => { data.current_by_kind.student_profiles = 0; },
    (data) => { data.active_course_targets = 0; data.active_course_current = 0; },
    (data) => { data.active_course_current--; },
    (data) => { data.known_session_targets = 0; data.known_session_current = 0; },
    (data) => { data.known_session_current--; },
    (data) => { data.expired_current = 1; },
    (data) => { data.active_permits = 1; },
    (data) => { data.host_paused_until = '2026-07-26T10:00:00Z'; },
  ];
  for (const mutate of mutations) {
    const response = readyResponse();
    mutate(response.data);
    assert.equal(isRolloutReady(response), false);
  }
  const failedEnvelope = readyResponse();
  failedEnvelope.success = false;
  assert.equal(isRolloutReady(failedEnvelope), false);
});
