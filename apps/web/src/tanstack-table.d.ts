import '@tanstack/react-table'

declare module '@tanstack/react-table' {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData, TValue> {
    className?: string // apply to both th and td
    tdClassName?: string
    thClassName?: string
    // Translated column name. The header renders its own title, but the
    // column-visibility menu has no header to read from and would otherwise
    // fall back to the raw column id.
    title?: string
  }
}
