import { useEffect, useRef } from 'react';

export const SNAPSHOT_COMMITTED_EVENT = 'snapshot-committed';

const snapshotVersions = new Map();

export function snapshotVersionKey(metadata) {
  if (!metadata?.kind || !metadata?.resource_key) return null;
  return `${metadata.kind}\0${metadata.parent_key || ''}\0${metadata.resource_key}`;
}

function validVersion(metadata) {
  return Number.isSafeInteger(metadata?.version) && metadata.version > 0;
}

export function applySnapshotStateSync(metadataItems) {
  if (!Array.isArray(metadataItems)) return;
  for (const metadata of metadataItems) {
    const key = snapshotVersionKey(metadata);
    if (!key || !validVersion(metadata)) continue;
    const current = snapshotVersions.get(key) || 0;
    if (metadata.version > current) {
      snapshotVersions.set(key, metadata.version);
    }
  }
}

export function publishSnapshotCommitted(metadata) {
  const key = snapshotVersionKey(metadata);
  if (!key || !validVersion(metadata)) return false;
  const current = snapshotVersions.get(key) || 0;
  if (metadata.version <= current) return false;
  snapshotVersions.set(key, metadata.version);
  window.dispatchEvent(new CustomEvent(
    SNAPSHOT_COMMITTED_EVENT,
    { detail: metadata }
  ));
  return true;
}

export function isCatalogSnapshot(metadata) {
  return metadata?.kind === 'course_catalog' &&
    metadata?.resource_key === 'catalog';
}

export function isCourseSnapshot(metadata, courseId) {
  return metadata?.kind === 'course_detail' &&
    metadata?.resource_key === courseId;
}

export function isSessionSnapshot(metadata, courseId, sessionId) {
  return metadata?.kind === 'session_detail' &&
    metadata?.parent_key === courseId &&
    metadata?.resource_key === sessionId;
}

export function isCourseSessionSnapshot(metadata, courseId) {
  return metadata?.kind === 'session_detail' &&
    metadata?.parent_key === courseId;
}

export function useSnapshotEvents(predicate, callback) {
  const predicateRef = useRef(predicate);
  const callbackRef = useRef(callback);
  predicateRef.current = predicate;
  callbackRef.current = callback;

  useEffect(() => {
    const handler = (event) => {
      if (predicateRef.current?.(event.detail)) {
        callbackRef.current?.(event.detail);
      }
    };
    window.addEventListener(SNAPSHOT_COMMITTED_EVENT, handler);
    return () => window.removeEventListener(SNAPSHOT_COMMITTED_EVENT, handler);
  }, []);
}

export function resetSnapshotVersionsForTests() {
  snapshotVersions.clear();
}
