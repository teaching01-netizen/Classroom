import { z } from 'zod'

export const snapshotMetadataSchema = z.object({
  kind: z.enum(['course_catalog', 'course_detail', 'session_detail']),
  resource_key: z.string().min(1),
  parent_key: z.string().optional(),
  version: z.number().int().positive(),
})

export type SnapshotMetadata = z.infer<typeof snapshotMetadataSchema>

const snapshotVersions = new Map<string, number>()

export function snapshotVersionKey(metadata: SnapshotMetadata): string {
  return `${metadata.kind}\0${metadata.parent_key ?? ''}\0${metadata.resource_key}`
}

export function applySnapshotStateSync(items: readonly SnapshotMetadata[]): void {
  for (const metadata of items) {
    const key = snapshotVersionKey(metadata)
    const current = snapshotVersions.get(key) ?? 0
    if (metadata.version > current) {
      snapshotVersions.set(key, metadata.version)
    }
  }
}

export function acceptSnapshotCommitted(metadata: SnapshotMetadata): boolean {
  const key = snapshotVersionKey(metadata)
  const current = snapshotVersions.get(key) ?? 0
  if (metadata.version <= current) {
    return false
  }
  snapshotVersions.set(key, metadata.version)
  return true
}

export function resetSnapshotVersionsForTests(): void {
  snapshotVersions.clear()
}
