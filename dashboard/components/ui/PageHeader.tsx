import { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';

// Every page starts with this: icon, title, an optional sentence, and the
// page's actions on the right.
export function PageHeader({
  icon: Icon,
  title,
  description,
  actions,
}: {
  icon: LucideIcon;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div className="min-w-0">
        <h1 className="text-xl font-semibold text-text-primary flex items-center gap-2">
          <Icon className="h-5 w-5 text-text-secondary flex-shrink-0" />
          <span className="truncate">{title}</span>
        </h1>
        {description && <p className="text-sm text-text-secondary mt-1 max-w-2xl">{description}</p>}
      </div>
      {actions && <div className="flex items-center gap-2 flex-wrap sm:justify-end">{actions}</div>}
    </div>
  );
}
