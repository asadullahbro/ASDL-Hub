'use client';

import { ReactNode, useEffect } from 'react';
import { X } from 'lucide-react';

// The dashboard's one dialog: a title, a body, and buttons in the footer.
export function Modal({
  open = true,
  title,
  onClose,
  children,
  footer,
  size = 'md',
}: {
  open?: boolean;
  title: ReactNode;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  size?: 'sm' | 'md' | 'lg' | 'xl';
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;
  const width = { sm: 'max-w-md', md: 'max-w-lg', lg: 'max-w-2xl', xl: 'max-w-4xl' }[size];
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4" onMouseDown={e => e.target === e.currentTarget && onClose()}>
      <div className={`bg-surface border border-border rounded-lg w-full ${width} max-h-[90vh] flex flex-col shadow-2xl`}>
        <div className="flex items-center justify-between gap-4 px-5 py-4 border-b border-border">
          <h2 className="text-sm font-semibold text-text-primary flex items-center gap-2 min-w-0">{title}</h2>
          <button onClick={onClose} aria-label="Close" className="text-text-secondary hover:text-text-primary p-1 -m-1 rounded">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="p-5 overflow-y-auto space-y-4">{children}</div>
        {footer && <div className="flex justify-end gap-2 px-5 py-3.5 border-t border-border">{footer}</div>}
      </div>
    </div>
  );
}

// Field is a labelled form control with optional help text.
export function Field({ label, help, children }: { label: ReactNode; help?: ReactNode; children: ReactNode }) {
  return (
    <label className="block">
      <span className="block text-xs text-text-secondary mb-1.5">{label}</span>
      {children}
      {help && <span className="block text-[11px] text-text-muted mt-1">{help}</span>}
    </label>
  );
}

// Shared classes for inputs, selects and tables.
export const inputClass =
  'w-full h-9 bg-background border border-border rounded-md px-3 text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent';
export const thClass = 'text-left py-2.5 px-4 text-[11px] font-medium text-text-secondary uppercase tracking-wider';
export const tdClass = 'py-3 px-4';
