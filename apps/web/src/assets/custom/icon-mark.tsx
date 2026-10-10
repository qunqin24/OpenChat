import { type SVGProps } from 'react'

/**
 * The OpenChat mark: six circles sharing one tangent point.
 *
 * Kept in step with `public/logo-mark.svg`, `public/favicon.svg` and the
 * inline copy in `index.html`; `currentColor` so it takes the ink colour of
 * whatever it sits in, rather than carrying its own.
 */
export function IconMark(props: SVGProps<SVGSVGElement>) {
  return (
    <svg
      data-name='icon-mark'
      xmlns='http://www.w3.org/2000/svg'
      viewBox='0 0 32 32'
      fill='none'
      {...props}
    >
      <g stroke='currentColor' strokeWidth={1.3}>
        <circle cx={29} cy={16} r={2} strokeOpacity={0.40} />
        <circle cx={26.4} cy={16} r={4.6} strokeOpacity={0.52} />
        <circle cx={23.8} cy={16} r={7.2} strokeOpacity={0.64} />
        <circle cx={21.2} cy={16} r={9.8} strokeOpacity={0.76} />
        <circle cx={18.6} cy={16} r={12.4} strokeOpacity={0.88} />
        <circle cx={16} cy={16} r={15} strokeOpacity={1.00} />
      </g>
    </svg>
  )
}
