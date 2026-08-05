import { expect, test, type Download } from '@playwright/test'
import { inflateRawSync } from 'node:zlib'
import { mockBackend } from './mock-backend'

async function downloadBuffer(download: Download): Promise<Buffer> {
  const stream = await download.createReadStream()
  const chunks: Buffer[] = []
  for await (const chunk of stream) {
    chunks.push(chunk as Buffer)
  }
  return Buffer.concat(chunks)
}

function parseCsv(buffer: Buffer): string[][] {
  const text = buffer.toString('utf8').replace(/^\ufeff/, '')
  return text
    .split('\n')
    .filter((line) => line.length > 0)
    .map((line) => line.split(','))
}

// Minimal ZIP reader good enough for the fixture workbook: central directory
// parsing plus deflate via node:zlib.
function readZipEntries(buffer: Buffer): Map<string, Buffer> {
  const entries = new Map<string, Buffer>()
  const eocdSignature = Buffer.from([0x50, 0x4b, 0x05, 0x06])
  let eocd = -1
  const searchStart = Math.max(0, buffer.length - 22 - 65_536)
  for (let index = buffer.length - 22; index >= searchStart; index--) {
    if (buffer.subarray(index, index + 4).equals(eocdSignature)) {
      eocd = index
      break
    }
  }
  if (eocd < 0) {
    throw new Error('no end-of-central-directory record found')
  }
  const centralOffset = buffer.readUInt32LE(eocd + 16)
  const centralSignature = Buffer.from([0x50, 0x4b, 0x01, 0x02])
  let cursor = centralOffset
  while (buffer.subarray(cursor, cursor + 4).equals(centralSignature)) {
    const method = buffer.readUInt16LE(cursor + 10)
    const compressedSize = buffer.readUInt32LE(cursor + 20)
    const nameLength = buffer.readUInt16LE(cursor + 28)
    const extraLength = buffer.readUInt16LE(cursor + 30)
    const commentLength = buffer.readUInt16LE(cursor + 32)
    const localOffset = buffer.readUInt32LE(cursor + 42)
    const name = buffer.subarray(cursor + 46, cursor + 46 + nameLength).toString('utf8')
    const localNameLength = buffer.readUInt16LE(localOffset + 26)
    const localExtraLength = buffer.readUInt16LE(localOffset + 28)
    const dataStart = localOffset + 30 + localNameLength + localExtraLength
    const compressed = buffer.subarray(dataStart, dataStart + compressedSize)
    entries.set(name, method === 8 ? inflateRawSync(compressed) : Buffer.from(compressed))
    cursor += 46 + nameLength + extraLength + commentLength
  }
  return entries
}

test('E2E-01 downloads a CSV export with the validated attendance data', async ({ page }) => {
  // Given
  await mockBackend(page)
  await page.goto('/courses/CS101/attendance')

  // When
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Export CSV' }).click()
  const download = await downloadPromise

  // Then
  expect(download.suggestedFilename()).toBe('attendance-software-engineering-2026-02-01.csv')
  const rows = parseCsv(await downloadBuffer(download))
  expect(rows).toHaveLength(3)
  expect(rows[0]).toEqual([
    'course_id', 'course_name', 'exported_at', 'source_validated_at',
    'student_id', 'student_name', 'nickname', 'school',
    'attended_sessions', 'total_sessions', 'attendance_rate', 'at_risk', 'Session 1',
  ])
  expect(rows[1]).toEqual([
    'CS101', 'Software Engineering', '2026-02-01T10:05:00Z', '2026-02-01T10:05:00Z',
    'u123', 'Alice Chen', 'Alice', 'Computer Science',
    '1', '1', '100.00%', 'false', 'Present',
  ])
  expect(rows[2]).toEqual([
    'CS101', 'Software Engineering', '2026-02-01T10:05:00Z', '2026-02-01T10:05:00Z',
    'u456', 'Sam Rivera', '', 'Computer Science',
    '0', '1', '0.00%', 'false', 'Absent',
  ])
})

test('E2E-02 downloads an XLSX workbook with Attendance and Metadata sheets', async ({ page }) => {
  // Given
  await mockBackend(page)
  await page.goto('/courses/CS101/attendance')

  // When
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Export Excel' }).click()
  const download = await downloadPromise

  // Then
  expect(download.suggestedFilename()).toBe('attendance-software-engineering-2026-02-01.xlsx')
  const entries = readZipEntries(await downloadBuffer(download))
  const workbookXml = entries.get('xl/workbook.xml')?.toString('utf8') ?? ''
  expect(workbookXml).toContain('name="Attendance"')
  expect(workbookXml).toContain('name="Metadata"')
  const sheetXml = entries.get('xl/worksheets/sheet1.xml')?.toString('utf8') ?? ''
  expect(sheetXml).toContain('state="frozen"')
  expect(sheetXml).toContain('filterMode="true"')
  const sharedStrings = entries.get('xl/sharedStrings.xml')?.toString('utf8') ?? ''
  expect(sharedStrings).toContain('Alice Chen')
  expect(sharedStrings).toContain('Sam Rivera')
  expect(sharedStrings).toContain('Software Engineering')
  expect(sharedStrings).toContain('CS101')
  expect(sharedStrings).toContain('Present')
  expect(sharedStrings).toContain('Absent')
})

test('E2E-03 exports freshly scraped data and the page reflects it after refetch', async ({ page }) => {
  // Given: the stored report is initially absent
  await mockBackend(page)
  await page.goto('/courses/CS101/attendance')
  const aliceRow = page.getByRole('row').filter({ hasText: 'u123' })
  await expect(aliceRow).toBeVisible({ timeout: 10_000 })
  await expect(aliceRow.getByText('×')).toBeVisible()

  // When: the export runs (which refreshes the scraped snapshot) and the page
  // refetches the report
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Export CSV' }).click()
  const download = await downloadPromise

  // Then: the export file contains the refreshed present value
  const rows = parseCsv(await downloadBuffer(download))
  expect(rows[1][12]).toBe('Present')
  // and the refetched page shows the present state
  await expect(aliceRow.getByText('✓')).toBeVisible()
  await expect(aliceRow.getByText('×')).toHaveCount(0)
})

test('E2E-04 shows an error and never downloads when the export refresh fails', async ({ page }) => {
  // Given
  await mockBackend(page, { exportMode: 'failure' })
  await page.goto('/courses/CS101/attendance')

  // When
  const downloadPromise = page.waitForEvent('download', { timeout: 2000 }).catch(() => null)
  await page.getByRole('button', { name: 'Export CSV' }).click()

  // Then
  await expect(
    page.getByRole('alert').filter({ hasText: 'Latest attendance data could not be validated' }),
  ).toBeVisible()
  expect(await downloadPromise).toBeNull()
})

test('E2E-05 shows loading state, disables controls, and sends one request despite repeated clicks', async ({ page }) => {
  // Given
  let exportRequests = 0
  page.on('request', (request) => {
    if (request.url().includes('/attendance-report/export')) {
      exportRequests++
    }
  })
  await mockBackend(page, { exportMode: 'delay', exportDelayMs: 1200 })
  await page.goto('/courses/CS101/attendance')
  const csvButton = page.getByRole('button', { name: 'Export CSV' })
  const excelButton = page.getByRole('button', { name: 'Export Excel' })

  // When: a second activation lands while the export is already in flight
  // (real browsers suppress clicks on the now-disabled button; the synthetic
  // dispatch below exercises the same guard deterministically)
  const downloadPromise = page.waitForEvent('download')
  await csvButton.click()
  await csvButton.dispatchEvent('click')

  // Then: loading state appears, both exports are disabled, and only one
  // request was made
  await expect(csvButton).toBeDisabled()
  await expect(excelButton).toBeDisabled()
  await expect(page.getByRole('status').filter({ hasText: 'Refreshing latest data' })).toBeVisible()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toBe('attendance-software-engineering-2026-02-01.csv')
  expect(exportRequests).toBe(1)
})

test('E2E-06 supports keyboard activation and announces progress', async ({ page }) => {
  // Given
  await mockBackend(page, { exportMode: 'delay', exportDelayMs: 700 })
  await page.goto('/courses/CS101/attendance')
  const csvButton = page.getByRole('button', { name: 'Export CSV' })
  const excelButton = page.getByRole('button', { name: 'Export Excel' })

  // Wait for the initial report fetch to finish so the export controls are
  // enabled before exercising tab order.
  await expect(csvButton).toBeEnabled()

  // When: tab navigation reaches the export controls in DOM order
  await page.getByRole('button', { name: 'Refresh report' }).focus()
  await page.keyboard.press('Tab')
  await expect(csvButton).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(excelButton).toBeFocused()

  // Then: Enter activates the Excel export, progress is announced, and focus
  // returns to the initiating control
  await excelButton.focus()
  const excelDownloadPromise = page.waitForEvent('download')
  await page.keyboard.press('Enter')
  await expect(page.getByRole('status').filter({ hasText: 'Refreshing latest data' })).toBeVisible()
  const excelDownload = await excelDownloadPromise
  expect(excelDownload.suggestedFilename()).toContain('.xlsx')
  await expect(excelButton).toBeFocused()

  // Space activates the CSV export as well
  await csvButton.focus()
  const csvDownloadPromise = page.waitForEvent('download')
  await page.keyboard.press('Space')
  const csvDownload = await csvDownloadPromise
  expect(csvDownload.suggestedFilename()).toContain('.csv')
  await expect(csvButton).toBeFocused()
})
