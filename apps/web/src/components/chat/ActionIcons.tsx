import type { ReactNode } from 'react'

/**
 * The icons under a message - copy, edit, regenerate, and the tick a copy
 * turns into - after OpenWebUI's: Iconoir's line icons (MIT), which it uses.
 * Drawn at 16px with a 1.5 stroke and small-radius corners (`.n-action-icon`),
 * they read finer than lucide's at 14px and 2: the weight of the text beside
 * them, not of a toolbar.
 */
function ActionIcon({ children, viewBox = '0 0 24 24' }: { children: ReactNode; viewBox?: string }) {
  return (
    <svg className="n-action-icon" viewBox={viewBox} aria-hidden="true">
      {children}
    </svg>
  )
}

/** Two sheets, one over the other. */
export function CopyIcon() {
  return (
    <ActionIcon>
      <path d="M19.4 20H9.6C9.27 20 9 19.73 9 19.4V9.6C9 9.27 9.27 9 9.6 9H19.4C19.73 9 20 9.27 20 9.6V19.4C20 19.73 19.73 20 19.4 20Z" />
      <path d="M15 9V4.6C15 4.27 14.73 4 14.4 4H4.6C4.27 4 4 4.27 4 4.6V14.4C4 14.73 4.27 15 4.6 15H9" />
    </ActionIcon>
  )
}

/** A copy made: the tick the copy button shows for a moment. */
export function CopiedIcon() {
  return (
    <ActionIcon>
      <path d="M5 13L9 17L19 7" />
    </ActionIcon>
  )
}

/** Two arcs round a circle, an arrow at each corner. */
export function RegenerateIcon() {
  return (
    // A wider box: OpenWebUI draws it a little smaller than the others.
    <ActionIcon viewBox="-2 -2 28 28">
      <path d="M21.17 8C19.62 4.47 16.1 2 12 2C6.81 2 2.55 5.95 2.05 11" />
      <path d="M17 8H21.4C21.73 8 22 7.73 22 7.4V3" />
      <path d="M2.88 16C4.42 19.53 7.95 22 12.05 22C17.23 22 21.5 18.05 22 13" />
      <path d="M7.05 16H2.65C2.32 16 2.05 16.27 2.05 16.6V21" />
    </ActionIcon>
  )
}

/** A pencil. */
export function EditIcon() {
  return (
    <ActionIcon>
      <path d="M14.36 5.65L15.84 4.17C16.62 3.39 17.89 3.39 18.67 4.17L20.09 5.59C20.87 6.37 20.87 7.63 20.09 8.41L18.61 9.89M14.36 5.65L4.75 15.27C4.42 15.6 4.21 16.04 4.17 16.51L3.93 19.25C3.87 19.87 4.39 20.38 5.01 20.33L7.75 20.09C8.22 20.05 8.66 19.84 8.99 19.51L18.61 9.89M14.36 5.65L18.61 9.89" />
    </ActionIcon>
  )
}
