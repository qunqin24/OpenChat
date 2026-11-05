import type { ComponentProps } from 'react'

/**
 * A Markdown table in an answer: one frame, which scrolls sideways when the
 * table is wide.
 *
 * Replaces Streamdown's own table, which nests the table in two more frames and
 * carries copy, download and full-screen controls whose menus are styled with
 * Tailwind classes this app never generates. It had a copy button of its own
 * once; copying the answer copies the table with it.
 */
export function MarkdownTable({ children, node: _node, ...props }: ComponentProps<'table'> & { node?: unknown }) {
  return (
    <div className="n-md-table">
      <div className="n-md-table-scroll">
        <table {...props}>{children}</table>
      </div>
    </div>
  )
}
