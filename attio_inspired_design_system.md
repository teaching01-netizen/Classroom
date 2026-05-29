# Attio-inspired Design System Extraction

> Extracted visually from the provided product screenshots. Use this as a practical implementation spec, not as an exact brand-copy. Replace Attio logos, icons, names, and proprietary assets with your own.

## 1. Product UI Direction

The interface is a dense, calm B2B SaaS workspace: spreadsheet-like data tables, left navigation, top action bars, import flows, onboarding screens, drawers, and workflow builder cards.

Core visual traits:

- Mostly white canvas with very soft grey/lavender table backgrounds.
- Compact spacing and high information density.
- Thin 1px borders instead of heavy shadows.
- Blue as the primary action, focus, and selected-state color.
- Rounded but restrained corners: 6–12px.
- Small typography, strong hierarchy through weight rather than size.
- Icons are line-based, 16px, monochrome or blue when active.

---

## 2. Design Tokens

### Colors

```css
:root {
  /* Brand / primary */
  --color-primary-600: #276BF0;
  --color-primary-650: #256CF1;
  --color-primary-700: #236AF5;
  --color-primary-hover: #286BEF;
  --color-primary-soft: #EAF0FE;
  --color-primary-soft-2: #E6EBFE;
  --color-primary-banner: #E5EEFF;
  --color-primary-text: #1E3C7D;

  /* Base surfaces */
  --color-bg: #FFFFFF;
  --color-bg-app: #FBFBFB;
  --color-bg-table: #FAF9FE;
  --color-bg-subtle: #F5F5F5;
  --color-bg-hover: #F1F2F4;
  --color-bg-selected: #EEEFF1;

  /* Borders */
  --color-border-subtle: #EEEFF1;
  --color-border: #DCDBDD;
  --color-border-strong: #CFCFD9;

  /* Text */
  --color-text-primary: #111113;
  --color-text-secondary: #4F5056;
  --color-text-muted: #696A6C;
  --color-text-placeholder: #AAAAAA;
  --color-text-disabled: #B8BCC4;
  --color-text-inverse: #FFFFFF;

  /* Utility */
  --color-success-bg: #DCF3E5;
  --color-warning-bg: #FAF0C4;
  --color-neutral-pill-bg: #F0F5CF;
  --color-purple-bg: #F0ECFE;
  --color-orange-bg: #F9E9E3;
  --color-red-bg: #F9E0E3;
  --color-blue-pill-bg: #E1E9FE;
}
```

### Typography

Recommended font stack:

```css
--font-sans: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
```

| Token | Size | Line height | Weight | Usage |
|---|---:|---:|---:|---|
| `text-xs` | 11px | 16px | 500 | step labels, helper labels, captions |
| `text-sm` | 12px | 16px | 500 / 600 | badges, table metadata, sidebar secondary text |
| `text-md` | 13px | 18px | 400 / 500 / 600 | nav items, form helper text, drawer copy |
| `text-base` | 14px | 20px | 400 / 500 / 600 | default UI text, table cells, buttons |
| `text-lg` | 16px | 24px | 600 | section headers, modal headings |
| `text-xl` | 20px | 28px | 600 / 700 | empty-state titles, onboarding titles |
| `text-2xl` | 24px | 32px | 700 | hero modal titles |

General typography rules:

- Use 14px as the default interactive text size.
- Use 12–13px for dense table/helper information.
- Primary headings are bold but not oversized.
- Muted text uses grey, not opacity, for clearer rendering.

### Spacing

Use a 4px base grid.

```css
--space-1: 4px;
--space-2: 8px;
--space-3: 12px;
--space-4: 16px;
--space-5: 20px;
--space-6: 24px;
--space-8: 32px;
--space-10: 40px;
--space-12: 48px;
--space-14: 56px;
```

### Radius

```css
--radius-xs: 4px;
--radius-sm: 6px;
--radius-md: 8px;
--radius-lg: 10px;
--radius-xl: 12px;
--radius-2xl: 16px;
--radius-full: 999px;
```

Usage:

- Pills and tags: 5–6px.
- Buttons and inputs: 6–8px.
- Cards, popovers, drawers: 10–12px.
- Modals: 12–16px.

### Borders and Shadows

```css
--border-width: 1px;
--shadow-xs: 0 1px 2px rgba(16, 24, 40, 0.06);
--shadow-sm: 0 2px 8px rgba(16, 24, 40, 0.08);
--shadow-md: 0 8px 24px rgba(16, 24, 40, 0.10);
--shadow-lg: 0 16px 48px rgba(16, 24, 40, 0.14);
--focus-ring: 0 0 0 3px rgba(39, 107, 240, 0.16);
```

The product generally favors borders over shadows. Use shadows mainly for drawers, modals, floating toolbars, and dropdowns.

---

## 3. Layout System

### App Shell

Desktop layout:

```css
.app-shell {
  display: grid;
  grid-template-columns: 260px 1fr;
  min-height: 100vh;
  background: var(--color-bg);
}
```

### Sidebar

- Width: `260px`.
- Background: `#FBFBFB`.
- Right border: `1px solid #EEEFF1`.
- Workspace switcher height: `48–56px`.
- Navigation item height: `32px`.
- Sidebar icon size: `16px`.
- Selected nav item: background `#EEEFF1`, radius `8px`, text `#111113`.
- Inactive nav item: transparent background, text `#111113` or muted for secondary rows.
- Hover nav item: `#F1F2F4`.
- Bottom account/trial block is pinned to bottom with a top divider.

### Main Header / Toolbar

- Height: `56px`.
- Background: `#FFFFFF`.
- Bottom border: `1px solid #EEEFF1`.
- Breadcrumb/title text: 14px, 600.
- Right actions: icon buttons, menu button, primary actions.

### Table Workspace

- Data table occupies full width and height.
- Top controls row height: `48–56px`.
- Table header row: `40px`.
- Body row: `40px` in dense mode, up to `44px` if descriptions/tags are present.
- Footer/calculation row: `40px`.
- Horizontal and vertical gridlines use `#EEEFF1`.

---

## 4. Core Components

### Buttons

#### Primary Button

```css
.btn-primary {
  height: 36px;
  padding: 0 14px;
  border-radius: 8px;
  border: 1px solid var(--color-primary-600);
  background: var(--color-primary-600);
  color: #FFFFFF;
  font-size: 14px;
  font-weight: 600;
  line-height: 20px;
  box-shadow: var(--shadow-xs);
}
.btn-primary:hover { background: var(--color-primary-hover); }
.btn-primary:active { background: var(--color-primary-700); }
.btn-primary:focus-visible { box-shadow: var(--focus-ring); }
.btn-primary:disabled {
  background: #B9D0FB;
  border-color: #B9D0FB;
  cursor: not-allowed;
}
```

Used for: Save, Continue, Start import, New workflow, Start trial.

#### Secondary Button

```css
.btn-secondary {
  height: 36px;
  padding: 0 14px;
  border-radius: 8px;
  border: 1px solid var(--color-border);
  background: #FFFFFF;
  color: var(--color-text-primary);
  font-size: 14px;
  font-weight: 500;
  box-shadow: var(--shadow-xs);
}
.btn-secondary:hover { background: var(--color-bg-hover); }
.btn-secondary:focus-visible { box-shadow: var(--focus-ring); }
```

Used for: Back, Start over, Choose file, Filter, View settings.

#### Ghost / Link Button

- Transparent background.
- Text color: `#696A6C` or primary blue for links.
- Hover: `#F1F2F4`.
- Used for “Continue without sync”, “Skip for now”, drawer links.

#### Icon Button

```css
.icon-button {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--color-text-muted);
}
.icon-button:hover { background: var(--color-bg-hover); }
```

### Inputs and Selects

```css
.input {
  height: 40px;
  width: 100%;
  border: 1px solid var(--color-border-subtle);
  border-radius: 8px;
  background: #FFFFFF;
  padding: 0 12px;
  color: var(--color-text-primary);
  font-size: 14px;
}
.input::placeholder { color: var(--color-text-placeholder); }
.input:hover { border-color: var(--color-border); }
.input:focus {
  outline: none;
  border-color: var(--color-primary-600);
  box-shadow: var(--focus-ring);
}
```

Selects/dropdowns use the same height and radius with a trailing chevron.

### Menus / Dropdowns

- Background: `#FFFFFF`.
- Border: `1px solid #EEEFF1`.
- Radius: `10px`.
- Shadow: `var(--shadow-md)`.
- Item height: `36–40px`.
- Item padding: `8px 12px`.
- Item hover/active background: `#F1F2F4`.
- Section labels: 12px, muted, semibold.
- Menu width: usually `260–320px` depending on content.

### Tables

```css
.data-table {
  width: 100%;
  border-collapse: separate;
  border-spacing: 0;
  background: #FFFFFF;
  font-size: 14px;
}
.data-table th,
.data-table td {
  height: 40px;
  padding: 0 12px;
  border-right: 1px solid var(--color-border-subtle);
  border-bottom: 1px solid var(--color-border-subtle);
  vertical-align: middle;
  white-space: nowrap;
}
.data-table th {
  color: var(--color-text-primary);
  font-weight: 600;
  background: #FFFFFF;
}
.data-table tr:hover td { background: var(--color-bg-table); }
.data-table tr[data-selected="true"] td { background: var(--color-primary-soft); }
```

Table behavior:

- First column usually contains checkbox + primary entity label.
- Header cells contain small icons for attribute type.
- AI/enriched columns use small sparkle icons.
- Empty values use muted placeholder text such as “No contact”.
- Bottom calculation row uses muted text and `+ Add calculation` controls.
- Selected rows are pale blue with blue checkboxes.

### Checkboxes

```css
.checkbox {
  width: 16px;
  height: 16px;
  border-radius: 4px;
  border: 1px solid var(--color-border-strong);
  background: #FFFFFF;
}
.checkbox[data-checked="true"] {
  background: var(--color-primary-700);
  border-color: var(--color-primary-700);
  color: #FFFFFF;
}
```

### Badges / Pills

Pills are compact, colored, and semibold.

```css
.pill {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 7px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
  line-height: 16px;
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
```

Suggested pill variants:

| Variant | Background | Suggested text |
|---|---|---|
| Green | `#DCF3E5` | `#257348` |
| Lime | `#F0F5CF` | `#6C7429` |
| Yellow | `#FAF0C4` | `#7A631C` |
| Orange | `#F9E9E3` | `#8B4A35` |
| Purple | `#F0ECFE` | `#5C3DC4` |
| Red | `#F9E0E3` | `#9A3D4A` |
| Blue | `#E1E9FE` | `#315EBA` |

### Empty States

Used in blank tables and workflow pages.

- Center aligned within available canvas.
- Icon: 32–48px, circular or simple line icon.
- Title: 18–20px, 600.
- Description: 13–14px, muted.
- Primary CTA below, often 36px high.
- Secondary connection buttons can sit above or beside the CTA.

### Drawers

Right-side help drawer:

```css
.drawer {
  width: 380px;
  background: #FFFFFF;
  border-left: 1px solid var(--color-border-subtle);
  box-shadow: -8px 0 24px rgba(16, 24, 40, 0.08);
}
```

Drawer structure:

- Header: title left, close icon right, 56px high.
- Content sections separated by 20–24px vertical spacing.
- Cards: white background, `1px #DCDBDD` border, `10px` radius, 12px padding.
- Small thumbnail blocks are neutral grey placeholders.

### Modals

Centered “Welcome to Pro” modal:

```css
.modal {
  width: 470px;
  max-width: calc(100vw - 32px);
  border-radius: 14px;
  background: #FFFFFF;
  border: 1px solid var(--color-border-subtle);
  box-shadow: var(--shadow-lg);
  padding: 48px 56px;
  text-align: center;
}
```

Modal rules:

- Keep copy short and centered.
- Icon above title, 32–40px.
- Title: 24px, 700.
- Body: 14px, muted, max width around 360px.
- Primary action centered below body.

### Floating Action Bar

Used when table rows are selected.

- Position: fixed bottom center.
- Height: `52–56px`.
- Background: `#FFFFFF`.
- Border: `1px solid #EEEFF1`.
- Radius: `12px`.
- Shadow: `var(--shadow-md)`.
- Contains selected count chip, actions, overflow menu, close icon.

### Stepper / Import Flow

Import wizard header:

- Full-width shell over the app area.
- Top row contains close icon + title.
- Step row height: `48px`.
- Step circles: 22px diameter.
- Active step: blue-soft background, blue text/border.
- Inactive step: grey circle, muted text.
- Separator chevrons between steps.
- Footer action bar fixed at bottom with Back / Start import.

Upload state:

- Centered drag-and-drop area.
- Large outline file/folder icon.
- Helper text 14px muted.
- Secondary “Choose .CSV file” button.

Preview generating state:

- Centered small illustration.
- Title: 14px semibold.
- Progress bar: 250–300px wide, 4px height, blue fill.
- Caption muted below.

### Onboarding Screens

- Logo top-center.
- Large centered card or two-column layout.
- Left column contains form/actions.
- Right column contains abstract product illustration in grey/blue.
- Progress counter: small muted text such as `1/5`.
- Primary CTA spans form width.
- OAuth buttons are full-width blue primary buttons when emphasized.
- Privacy/legal text is 11–12px muted.

### Workflow Builder

Canvas:

- White background.
- Top banner for unpublished state: `#E5EEFF`, blue text, primary CTA right.
- Blocks are centered vertically with connectors.
- Block card: width `280–320px`, min-height `64px`, white background, 1px subtle border, 8px radius.
- Trigger label appears as a small top label inside/above first card.
- Connector line: 1px grey vertical line.
- Add area: large dashed rectangle, blue dashed border, radius 10px, plus icon centered.

Right panel:

- Width: `320–360px`.
- Left border: `1px #EEEFF1`.
- Header contains back arrow.
- Search input height 40px.
- Blocks grouped by category with section labels.
- Block option height: 52–56px, border, radius 8–10px, hover background `#F1F2F4`.

---

## 5. Interaction States

| State | Treatment |
|---|---|
| Hover | `#F1F2F4` for nav/buttons, `#FAF9FE` for table rows |
| Active/selected nav | `#EEEFF1`, 8px radius |
| Selected table row | `#EAF0FE` or `#E6EBFE` |
| Focus | Blue border + `0 0 0 3px rgba(39, 107, 240, 0.16)` |
| Disabled | Low contrast grey text `#B8BCC4`, pale blue disabled button |
| Loading | Small centered illustration + thin blue progress bar |
| Error | Not visible in screenshots; use red text `#B42318`, bg `#FEF3F2` |

---

## 6. Iconography

- Use 16px icons for nav, table headers, menus, and buttons.
- Use 20–24px icons for empty states and onboarding callouts.
- Stroke weight: 1.5–2px.
- Style: rounded line icons, minimal filled icons only for active blue states.
- Icon colors:
  - Default: `#696A6C`.
  - Muted: `#AAAAAA`.
  - Active: `#276BF0`.
  - Inverse: `#FFFFFF`.

Recommended libraries: Lucide, Phosphor, or similar line-icon set.

---

## 7. Accessibility Rules

- Minimum interactive target: 32px, preferred 36–40px.
- Always show visible keyboard focus using the blue focus ring.
- Do not rely only on color for selected state; include checkbox/checkmark or active icon.
- Table row selected state should be announced through `aria-selected`.
- Menus should support keyboard navigation and escape-to-close.
- Inputs require labels, even if labels are visually hidden.

---

## 8. Implementation Starter

```css
body {
  margin: 0;
  font-family: var(--font-sans);
  background: var(--color-bg);
  color: var(--color-text-primary);
  font-size: 14px;
  line-height: 20px;
  -webkit-font-smoothing: antialiased;
  text-rendering: optimizeLegibility;
}

.card,
.popover,
.modal,
.drawer,
.floating-bar {
  background: #FFFFFF;
  border: 1px solid var(--color-border-subtle);
}

.divider-x { border-bottom: 1px solid var(--color-border-subtle); }
.divider-y { border-right: 1px solid var(--color-border-subtle); }

.truncate {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
```

---

## 9. Page Templates

### CRM Table Page

- App shell with sidebar + main content.
- Top header: object title, workspace avatar, comments/menu icons.
- View selector under header, left aligned.
- Toolbar with sort/filter controls.
- Table with entity column, tags/categories, links, last interaction, connection strength.
- Save/discard controls when table configuration changes.

### Import Wizard

- Full-screen overlay/panel inside app shell.
- Header with close icon and import title.
- Horizontal stepper.
- Centered upload/mapping/preview content.
- Bottom sticky footer with Back and primary CTA.

### Onboarding

- Standalone centered layout.
- Logo top.
- Form on left, abstract table illustration on right.
- Footer legal links centered.

### Workflow Builder

- Sidebar remains visible.
- Workflow canvas in center.
- Right properties/blocks panel.
- Top unpublished banner.
- Floating zoom/tools at bottom center.
