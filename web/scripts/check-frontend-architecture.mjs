import { readdir, readFile } from 'node:fs/promises'
import { extname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import process from 'node:process'

const sourceRoot = fileURLToPath(new globalThis.URL('../src/', import.meta.url))
const architectureRoots = ['app', 'features', 'shared']
const sourceExtensions = new Set(['.ts', '.tsx'])
// The realtime layer is a domain hub that must share the rooms feature's
// query keys and zod response schemas (egress-reduction plan, bug.md
// Phase 3.3). Keep this allowance as narrow as possible.
const sharedFeatureImportsAllowedPrefixes = ['shared/realtime/']
const failures = []

async function filesUnder(directory) {
  const entries = await readdir(directory, { withFileTypes: true })
  const nested = await Promise.all(entries.map(async (entry) => {
    const path = join(directory, entry.name)
    return entry.isDirectory() ? filesUnder(path) : [path]
  }))
  return nested.flat()
}

for (const root of architectureRoots) {
  const directory = join(sourceRoot, root)
  const files = (await filesUnder(directory)).filter((file) => sourceExtensions.has(extname(file)))
  for (const file of files) {
    const source = await readFile(file, 'utf8')
    const label = relative(sourceRoot, file)
    if (
      root === 'shared'
      && /from ['"]@\/features\//.test(source)
      && !sharedFeatureImportsAllowedPrefixes.some((prefix) => label.startsWith(prefix))
    ) {
      failures.push(`${label}: shared code imports a feature`)
    }
    if (/\/routes\//.test(file) && /\bfetch\s*\(/.test(source)) {
      failures.push(`${label}: routed pages must not call fetch directly`)
    }
    if (root === 'features' && /from ['"]zustand/.test(source)) {
      failures.push(`${label}: server feature state must not use Zustand`)
    }
    if (root === 'features') {
      const crossFeatureInternals = source.matchAll(
        /from ['"]@\/features\/([^/'"]+)\/([^'"]+)['"]/g,
      )
      for (const match of crossFeatureInternals) {
        if (match[2] !== undefined) {
          failures.push(`${label}: import another feature through its public entry point`)
        }
      }
    }
  }
}

if (failures.length > 0) {
  process.stderr.write(`${failures.join('\n')}\n`)
  process.exit(1)
}

process.stdout.write('Frontend architecture guardrails passed.\n')
