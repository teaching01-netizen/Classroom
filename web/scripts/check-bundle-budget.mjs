import { readdir, stat } from 'node:fs/promises'
import process from 'node:process'

const assetsDirectory = new globalThis.URL('../dist/assets/', import.meta.url)
const files = await readdir(assetsDirectory)
const budgets = {
  '.js': 400_000,
  '.css': 50_000,
}
const failures = []

for (const file of files) {
  const extension = file.endsWith('.js') ? '.js' : file.endsWith('.css') ? '.css' : null
  if (extension === null) {
    continue
  }
  const size = (await stat(new globalThis.URL(file, assetsDirectory))).size
  const budget = budgets[extension]
  if (size > budget) {
    failures.push(`${file}: ${size} bytes exceeds ${budget} byte budget`)
  }
}

if (failures.length > 0) {
  process.stderr.write(`${failures.join('\n')}\n`)
  process.exit(1)
}

process.stdout.write('Bundle budgets passed.\n')
