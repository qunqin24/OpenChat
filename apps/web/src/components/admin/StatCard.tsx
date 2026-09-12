import type { ReactNode } from 'react'

interface StatCardProps {
  title: string
  icon: ReactNode
  value: ReactNode
  /** Secondary line under the figure, e.g. a 7-day delta. */
  hint?: ReactNode
  loading?: boolean
}

export function StatCard({ title, icon, value, hint, loading }: StatCardProps) {
  return (
    <div className="n-log-stat">
      <div className="n-log-stat-head">
        <span className="n-log-stat-title">{title}</span>
        <span className="n-log-stat-icon">{icon}</span>
      </div>

      {loading ? (
        <span className="n-log-shimmer" aria-hidden="true" />
      ) : (
        <span className="n-log-stat-value">{value}</span>
      )}

      {hint && !loading && <span className="n-log-stat-label">{hint}</span>}
    </div>
  )
}

export default StatCard
