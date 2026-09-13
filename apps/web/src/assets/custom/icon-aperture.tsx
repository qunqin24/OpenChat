import { type SVGProps } from 'react'

/**
 * The OpenChat mark: a three-bladed aperture.
 *
 * Kept in step with `public/favicon.svg` and the inline copy in `index.html`;
 * `currentColor` so it takes the ink colour of whatever it sits in, rather than
 * carrying its own.
 */
export function IconAperture(props: SVGProps<SVGSVGElement>) {
  return (
    <svg
      data-name='icon-aperture'
      xmlns='http://www.w3.org/2000/svg'
      viewBox='0 0 32 32'
      fill='none'
      {...props}
    >
      <g stroke='currentColor' strokeWidth={2.4} strokeLinecap='round'>
        <circle cx={16} cy={16} r={11.5} />
        <path d='M20.5 5.42V26.58' />
        <path d='M4.59 14.61L22.91 25.19' />
        <path d='M22.91 6.81L4.59 17.39' />
      </g>
    </svg>
  )
}
