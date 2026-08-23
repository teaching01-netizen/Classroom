# Check-in QR Command Center Design System

## 1. Atmosphere & Identity

A calm, precise operations surface for managing classroom attendance. The signature is a quiet white command bar above a soft neutral canvas: blue is reserved for the current destination and decisive actions, while green communicates a healthy live connection.

## 2. Color

| Role | Token | Light value | Usage |
|---|---|---|---|
| Canvas | `--color-surface-canvas` | `--gray-25` | Page background |
| Surface | `--color-surface-default` | `--gray-0` | Cards, top bar, dialogs |
| Subtle surface | `--color-surface-subtle` | `--gray-50` | Table headings and secondary regions |
| Border | `--color-border-default` | `--gray-200` | Separators and controls |
| Primary text | `--color-text-default` | `--gray-950` | Headings and body text |
| Secondary text | `--color-text-subtle` | `--gray-600` | Metadata and inactive navigation |
| Primary action | `--color-action-primary` | `--blue-600` | Active navigation, links, CTAs |
| Active action surface | `--color-action-soft` | `--blue-50` | Active navigation background |
| Success | `--color-positive` | `--green-700` | Live status and confirmations |
| Warning | `--color-warning` | `--amber-800` | Caution states |
| Danger | `--color-danger` | `--red-700` | Errors and destructive actions |

Colors are defined in `src/shared/styles/tokens.css`; new colors require a semantic token first.

## 3. Typography

- Primary: `Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif`
- Mono: `SFMono-Regular, Consolas, Liberation Mono, monospace`
- Scale: `--text-xs` 12px, `--text-sm` 14px, `--text-md` 16px, `--text-lg` 18px, `--text-xl` 24px, `--text-2xl` responsive 28–36px.
- Body text remains 14px or larger; page headings use the responsive `--text-2xl` token.

## 4. Spacing & Layout

Spacing uses a four-pixel base with `--space-1` through `--space-12`. The content limiter is 90rem with responsive `clamp(--space-4, 4vw, --space-8)` page gutters. The application shell owns document scrolling: the fixed top bar remains visible and the page body scrolls beneath it. At 48rem and below, the top bar is explicitly composed as two rows: brand and utilities, followed by an equal-width three-item navigation grid.

## 5. Components

### Top navigation shell

- **Structure**: branded home link, primary navigation, density selector, connection status, optional offline banner, main content.
- **States**: active route uses `--color-action-soft` and `--color-action-primary`; hover uses `--color-surface-hover`; focus uses the global visible focus ring.
- **Accessibility**: semantic `header` and labelled `nav`; skip link targets the main landmark; connection status has text as well as color.
- **Layout**: fixed document-level top bar; content is the sole scroll owner.

### Global data sync control

- **Structure**: compact secondary action in the top-bar utilities, available on every course-facing route.
- **States**: idle, busy with a spinner and `aria-busy`, and success or partial-failure feedback through the toast region.
- **Behavior**: the server refreshes the catalog first, then discovered course, session, and profile snapshots; active queries refetch after the command completes so newly discovered courses appear immediately.

### Shared primitives

Buttons, fields, selects, badges, tables, dialogs, toasts, empty states, errors, pagination, avatars, page headers, and statistics grids live in `src/shared/ui`. They use semantic tokens, keyboard support, visible focus states, and loading/error/empty variants where relevant.

## 6. Motion & Interaction

`--motion-fast` (140ms ease) is used for hover feedback and `--motion-medium` (220ms ease) for standard transitions. Motion is limited to color, transform, and opacity; reduced-motion preferences are honored in `themes.css`.

## 7. Depth & Surface

The surface strategy is mixed but restrained: thin `--color-border-default` separators establish structure, with `--shadow-sm` for raised cards and `--shadow-md` for dialogs. The top bar uses a translucent surface and blur to retain context without becoming a separate visual panel.

## 8. Accessibility Constraints & Accepted Debt

- Target: WCAG 2.2 AA, including visible keyboard focus, semantic landmarks, labelled controls, readable text contrast, and reduced-motion support.
- Mobile navigation must expose all three destinations without horizontal scrolling or clipped labels.
- No accepted design debt for the top-navigation shell.
