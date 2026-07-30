import type { QueryKey } from '@tanstack/react-query'
import type { SnapshotMetadata } from './snapshot-events'

export function getAffectedQueryKeys(metadata: SnapshotMetadata): readonly QueryKey[] {
  switch (metadata.kind) {
    case 'course_catalog':
      return [['courses']]
    case 'course_detail':
      return [
        ['courses'],
        ['courses', 'detail', metadata.resource_key],
        ['attendance', metadata.resource_key],
        ['absence-dashboard'],
      ]
    case 'session_detail': {
      const parentKey = metadata.parent_key
      if (parentKey === undefined) {
        return [['sessions']]
      }
      return [
        ['courses', 'detail', parentKey],
        ['sessions', parentKey],
        ['sessions', parentKey, metadata.resource_key],
        ['attendance', parentKey],
        ['absence-dashboard'],
      ]
    }
  }
}
