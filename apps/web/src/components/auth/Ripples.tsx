import type { CSSProperties } from 'react'

/**
 * The OpenChat mark's construction — circles growing out of one point — drawn
 * at the scale of a whole surface. Decorative only (`aria-hidden`); all motion
 * is CSS (auth.css) and is dropped under reduced motion, leaving the rings at
 * rest.
 */

interface Ring {
  r: number
  cx: number
  opacity: number
}

function rings(count: number, first: number, growth: number, ring: (r: number, i: number) => Omit<Ring, 'r'>) {
  const out: Ring[] = []
  let r = first
  for (let i = 0; i < count; i++) {
    out.push({ r, ...ring(r, i) })
    r *= growth
  }
  return out
}

/** As the mark draws them: all touching one point on the right edge, brightening outward. */
const PANEL_RINGS = rings(22, 8, 1.24, (r, i) => ({
  cx: 1000 - r,
  opacity: 0.3 + 0.62 * (i / 21),
}))

/** Centred on one point and fading outward: a signal going out while it waits. */
const WAIT_RINGS = rings(14, 60, 1.28, (_r, i) => ({
  cx: 0,
  opacity: 0.34 - 0.2 * (i / 13),
}))

function RingSet({ rings, cy }: { rings: Ring[]; cy: number }) {
  return rings.map((ring, i) => (
    <circle
      key={ring.r}
      cx={ring.cx}
      cy={cy}
      r={ring.r}
      style={{ '--ring-opacity': ring.opacity.toFixed(3), '--ring-index': i } as CSSProperties}
    />
  ))
}

/**
 * Fills the sign-in panel, anchored to its right edge. On arrival the rings
 * unfold once from their shared point, smallest first, each slowing as it
 * spreads like a ripple on still water — then they stay still.
 */
export function PanelRipples() {
  return (
    <svg
      className="n-auth-ripples"
      viewBox="0 0 1000 1000"
      preserveAspectRatio="xMaxYMid slice"
      aria-hidden="true"
    >
      <RingSet rings={PANEL_RINGS} cy={560} />
    </svg>
  )
}

/** Spreads across the window from the badge of the waiting screen. */
export function WaitRipples() {
  return (
    <svg className="n-account-overlay-ripples" viewBox="-1200 -1200 2400 2400" aria-hidden="true">
      <RingSet rings={WAIT_RINGS} cy={0} />
    </svg>
  )
}
