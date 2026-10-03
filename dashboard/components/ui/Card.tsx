import { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';

// A section of a page: a titled card with an optional sentence and actions.
export function Card({
  title,
  icon: Icon,
  description,
  actions,
  children,
  flush = false,
  className = '',
}: {
  title?: ReactNode;
  icon?: LucideIcon;
  description?: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
  // flush: no padding around the body (for lists and tables).
  flush?: boolean;
  className?: string;
}) {
  return (
    <section className={`bg-surface border border-border rounded-lg ${className}`}>
      {(title || actions) && (
        <div className="flex items-start justify-between gap-4 px-5 py-4 border-b border-border">
          <div className="min-w-0">
            {title && (
              <h2 className="text-sm font-semibold text-text-primary flex items-center gap-2">
                {Icon && <Icon className="h-4 w-4 text-text-secondary" />}
                {title}
              </h2>
            )}
            {description && <p className="text-xs text-text-secondary mt-1">{description}</p>}
          </div>
          {actions && <div className="flex items-center gap-2 flex-shrink-0">{actions}</div>}
        </div>
      )}
      {children !== undefined && children !== null && children !== false && <div className={flush ? '' : 'p-5'}>{children}</div>}
    </section>
  );
}

// StatCard is a number with a label, used in rows at the top of pages.
export function StatCard({
  label,
  value,
  sub,
  tone,
}: {
  label: ReactNode;
  value: ReactNode;
  sub?: ReactNode;
  tone?: 'success' | 'warning' | 'danger' | 'info';
}) {
  const color = tone
    ? { success: 'text-status-green', warning: 'text-status-yellow', danger: 'text-status-red', info: 'text-status-blue' }[tone]
    : 'text-text-primary';
  return (
    <div className="bg-surface border border-border rounded-lg px-4 py-3.5">
      <div className="text-xs text-text-secondary">{label}</div>
      <div className={`text-2xl font-semibold mt-1 tabular-nums ${color}`}>{value}</div>
      {sub && <div className="text-xs text-text-secondary mt-0.5">{sub}</div>}
    </div>
  );
}

// EmptyState fills a card or page that has nothing to show yet.
export function EmptyState({ children }: { children: ReactNode }) {
  return <div className="text-sm text-text-secondary text-center py-10">{children}</div>;
}
