# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: visual-evidence.spec.ts >> captures every route and QR state at every responsive breakpoint
- Location: e2e/visual-evidence.spec.ts:14:1

# Error details

```
Error: expect(locator).toBeVisible() failed

Locator: getByRole('dialog', { name: 'Student check-in QR code' })
Expected: visible
Timeout: 5000ms
Error: element(s) not found

Call log:
  - Expect "toBeVisible" with timeout 5000ms
  - waiting for getByRole('dialog', { name: 'Student check-in QR code' })

```

```yaml
- link "Skip to content":
  - /url: "#main-content"
- banner:
  - link "Check-in Command Center home":
    - /url: /
    - text: Check-in Center
  - navigation "Primary":
    - link "Dashboard":
      - /url: /
    - link "Absence alerts":
      - /url: /absence-dashboard
    - link "All courses":
      - /url: /courses
  - text: Density
  - combobox "Density":
    - option "Comfortable" [selected]
    - option "Compact"
  - text: Live
- main:
  - link "← Back to sessions":
    - /url: /courses/CS101/sessions
  - paragraph: Live check-in
  - heading "Architecture workshop" [level=2]
  - paragraph: Software Engineering
  - button "View QR code" [disabled]
  - button "Export CSV"
  - term: Checked in
  - definition: 1/2
  - term: Attendance rate
  - definition: 50%
  - text: Search students
  - searchbox "Search students"
  - text: Check-in status
  - combobox "Check-in status":
    - option "All students" [selected]
    - option "Checked in"
    - option "Not checked in"
  - table:
    - rowgroup:
      - row "Student School Status Points Action":
        - columnheader "Student"
        - columnheader "School"
        - columnheader "Status"
        - columnheader "Points"
        - columnheader "Action"
    - rowgroup:
      - row "Alice u123 Computer Science Checked in 4 Undo":
        - cell "Alice u123":
          - strong: Alice
          - text: u123
        - cell "Computer Science"
        - cell "Checked in"
        - cell "4"
        - cell "Undo":
          - button "Undo"
      - row "Sam Rivera u456 Computer Science Not checked in 1 Check in":
        - cell "Sam Rivera u456":
          - strong: Sam Rivera
          - text: u456
        - cell "Computer Science"
        - cell "Not checked in"
        - cell "1"
        - cell "Check in":
          - button "Check in"
```

# Test source

```ts
  1   | import { expect, test } from '@playwright/test'
  2   | import { mockBackend } from './mock-backend'
  3   | 
  4   | const evidenceDirectory = '.omo/evidence/top-navigation'
  5   | 
  6   | const viewports = [
  7   |   { name: 'desktop', width: 1440, height: 900 },
  8   |   { name: 'tablet', width: 768, height: 1024 },
  9   |   { name: 'mobile', width: 390, height: 844 },
  10  | ] as const
  11  | 
  12  | test.setTimeout(90_000)
  13  | 
  14  | test('captures every route and QR state at every responsive breakpoint', async ({ page }) => {
  15  |   // Given
  16  |   await mockBackend(page)
  17  |   // When
  18  |   for (const viewport of viewports) {
  19  |     await page.setViewportSize({ width: viewport.width, height: viewport.height })
  20  |     await captureRoute(page, viewport.name, 'home', '/', 'Your teaching dashboard')
  21  |     await captureRoute(page, viewport.name, 'courses', '/courses', 'All courses')
  22  |     await captureRoute(
  23  |       page,
  24  |       viewport.name,
  25  |       'sessions',
  26  |       '/courses/CS101/sessions',
  27  |       'Software Engineering',
  28  |     )
  29  |     await page.goto('/courses/CS101/sessions/S1')
  30  |     const dialog = page.getByRole('dialog', { name: 'Student check-in QR code' })
> 31  |     await expect(dialog).toBeVisible()
      |                          ^ Error: expect(locator).toBeVisible() failed
  32  |     await page.screenshot({
  33  |       path: `${evidenceDirectory}/${viewport.name}/checkin-qr.png`,
  34  |     })
  35  |     await dialog.getByRole('button', { name: 'Close dialog' }).click()
  36  |     await expect(dialog).not.toBeVisible()
  37  |     await captureCurrentPage(page, viewport.name, 'checkin')
  38  |     await captureRoute(
  39  |       page,
  40  |       viewport.name,
  41  |       'attendance',
  42  |       '/courses/CS101/attendance',
  43  |       'Software Engineering',
  44  |     )
  45  |     await captureRoute(
  46  |       page,
  47  |       viewport.name,
  48  |       'absence-dashboard',
  49  |       '/absence-dashboard?courses=CS101&threshold=1&load=1',
  50  |       'Student absence dashboard',
  51  |     )
  52  |   }
  53  |   await page.setViewportSize({ width: 1440, height: 900 })
  54  |   await page.goto('/courses')
  55  |   await expect(page.getByRole('heading', { name: 'All courses' })).toBeVisible()
  56  |   const navigationLink = page.getByRole('link', { name: 'Dashboard' })
  57  |   await page.screenshot({
  58  |     path: `${evidenceDirectory}/motion/nav-rest.png`,
  59  |   })
  60  |   await navigationLink.hover()
  61  |   await page.waitForTimeout(70)
  62  |   await page.screenshot({
  63  |     path: `${evidenceDirectory}/motion/nav-mid.png`,
  64  |   })
  65  |   await page.waitForTimeout(160)
  66  |   await page.screenshot({
  67  |     path: `${evidenceDirectory}/motion/nav-settled.png`,
  68  |   })
  69  |   // Then
  70  |   await expect(page.getByRole('heading', { name: 'All courses' })).toBeVisible()
  71  | })
  72  | 
  73  | async function captureRoute(
  74  |   page: import('@playwright/test').Page,
  75  |   viewport: string,
  76  |   name: string,
  77  |   path: string,
  78  |   heading: string,
  79  | ): Promise<void> {
  80  |   await page.goto(path)
  81  |   await expect(page.getByRole('heading', { name: heading })).toBeVisible()
  82  |   await captureCurrentPage(page, viewport, name)
  83  | }
  84  | 
  85  | async function captureCurrentPage(
  86  |   page: import('@playwright/test').Page,
  87  |   viewport: string,
  88  |   name: string,
  89  | ): Promise<void> {
  90  |   await page.evaluate(() => {
  91  |     window.scrollTo(0, 0)
  92  |     document.scrollingElement?.scrollTo(0, 0)
  93  |   })
  94  |   await expect.poll(() => page.evaluate(() => [window.scrollX, window.scrollY])).toEqual([0, 0])
  95  |   await expect(page.locator('.app-topbar')).toBeVisible()
  96  |   await page.waitForTimeout(100)
  97  |   await page.screenshot({
  98  |     path: `${evidenceDirectory}/${viewport}/${name}.png`,
  99  |   })
  100 |   const hasBelowFoldContent = await page.evaluate(
  101 |     () => document.documentElement.scrollHeight > window.innerHeight + 1,
  102 |   )
  103 |   if (hasBelowFoldContent) {
  104 |     await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight))
  105 |     await expect(page.locator('.app-topbar')).toBeVisible()
  106 |     await page.waitForTimeout(100)
  107 |     await page.screenshot({
  108 |       path: `${evidenceDirectory}/${viewport}/${name}-bottom.png`,
  109 |     })
  110 |   }
  111 | }
  112 | 
```