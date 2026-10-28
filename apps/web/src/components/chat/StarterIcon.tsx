import type { ReactNode } from 'react'

/** The new chat's starters (WelcomeStarters), each with its own drawing. */
export type Starter = 'explain' | 'code' | 'polish' | 'plan' | 'compare' | 'ideas'

/**
 * The drawings, made for these pills rather than taken from an icon set: line
 * art on a 24 grid, round caps and joins. Each has one finishing touch — the
 * star on the book, the cursor between the brackets, the sparkle by the nib,
 * the ticked item, the right-hand pan, the bolt — classed `is-accent` (a
 * stroke) or `is-accent-fill` (a shape), which the stylesheet colours by
 * starter while the rest takes the text colour (`.n-drawing`).
 */
const DRAWINGS: Record<Starter, ReactNode> = {
  // An open book with a star on its right-hand page.
  explain: (
    <>
      <path d="M12 6.5C10.3 5.3 7.8 4.8 4.5 5v12.5c3.3-.2 5.8.3 7.5 1.5 1.7-1.2 4.2-1.7 7.5-1.5V5c-3.3-.2-5.8.3-7.5 1.5Z" />
      <path d="M12 6.5V19" />
      <path className="is-accent-fill" d="M16 7.8c.25 1.36.84 1.95 2.2 2.2-1.36.25-1.95.84-2.2 2.2-.25-1.36-.84-1.95-2.2-2.2 1.36-.25 1.95-.84 2.2-2.2Z" />
    </>
  ),
  // Angle brackets with a text cursor between them.
  code: (
    <>
      <path d="M8.5 7.5 4 12l4.5 4.5" />
      <path d="M15.5 7.5 20 12l-4.5 4.5" />
      <path className="is-accent" d="M12 8.5v7" />
    </>
  ),
  // A pen nib with a sparkle beside it.
  polish: (
    <>
      <path d="M12 20.5 7.5 13.5 9.5 7h5l2 6.5Z" />
      <path d="M12 20.5V15" />
      <circle cx="12" cy="13" r="1.2" />
      <path d="M9.5 7V4h5v3" />
      <path className="is-accent-fill" d="M19.3 14.3c.25 1.36.84 1.95 2.2 2.2-1.36.25-1.95.84-2.2 2.2-.25-1.36-.84-1.95-2.2-2.2 1.36-.25 1.95-.84 2.2-2.2Z" />
    </>
  ),
  // A clipboard checklist: one item ticked, one still to do.
  plan: (
    <>
      <rect x="5" y="4.5" width="14" height="16" rx="2.5" />
      <rect x="9" y="3" width="6" height="3" rx="1" />
      <path className="is-accent" d="m8.2 11.3 1.4 1.4 2.4-2.6" />
      <path d="M14 11.5h2" />
      <circle cx="9.6" cy="16.2" r="1.1" />
      <path d="M14 16.2h2" />
    </>
  ),
  // A pair of scales, its right-hand pan the coloured one.
  compare: (
    <>
      <path d="M12 5.5v13.5M8.5 19.5h7" />
      <path d="M4.5 7.5h15" />
      <circle cx="12" cy="5" r="1" />
      <path d="M4.5 7.5 2.6 12.5M4.5 7.5l1.9 5" />
      <path d="M2.4 12.5a2.1 2.1 0 0 0 4.2 0Z" />
      <path className="is-accent" d="m19.5 7.5-1.9 5M19.5 7.5l1.9 5" />
      <path className="is-accent-fill" d="M17.4 12.5a2.1 2.1 0 0 0 4.2 0Z" />
    </>
  ),
  // A cloud with a bolt from it.
  ideas: (
    <>
      <path d="M8 16.5H7a3.6 3.6 0 0 1-.5-7.2A5.2 5.2 0 0 1 16.4 8a3.9 3.9 0 0 1 1 7.7" />
      <path className="is-accent" d="M13 11.5 10.8 15.3h3.2l-2 3.9" />
    </>
  ),
}

export function StarterIcon({ starter }: { starter: Starter }) {
  return (
    <svg className="n-drawing n-starter-icon" viewBox="0 0 24 24" aria-hidden="true">
      {DRAWINGS[starter]}
    </svg>
  )
}

export default StarterIcon
