import { ReactNode, ButtonHTMLAttributes } from 'react';
import type { LucideIcon } from 'lucide-react';

// The dashboard's one button. primary: the main action of a page or dialog;
// secondary: everything else; danger: removes or revokes something; ghost:
// quiet actions in toolbars and rows.
interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  children?: ReactNode;
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost';
  size?: 'sm' | 'md';
  icon?: LucideIcon;
  loading?: boolean;
}

const variantStyles = {
  primary: 'bg-accent text-black border border-accent hover:bg-accent-hover hover:border-accent-hover',
  secondary: 'bg-surface border border-border hover:bg-surface-hover hover:border-border-strong text-text-primary',
  danger: 'bg-transparent border border-status-red/40 text-status-red hover:bg-status-red/10',
  ghost: 'border border-transparent text-text-secondary hover:bg-surface-hover hover:text-text-primary',
};

const sizeStyles = {
  sm: 'h-8 px-3 text-xs gap-1.5',
  md: 'h-9 px-4 text-sm gap-2',
};

export function Button({
  children,
  variant = 'secondary',
  size = 'sm',
  icon: Icon,
  loading = false,
  className = '',
  disabled,
  type = 'button',
  ...props
}: ButtonProps) {
  return (
    <button
      type={type}
      className={`inline-flex items-center justify-center rounded-md font-medium transition-colors whitespace-nowrap
        focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent
        disabled:opacity-50 disabled:pointer-events-none ${variantStyles[variant]} ${sizeStyles[size]} ${className}`}
      disabled={disabled || loading}
      {...props}
    >
      {loading ? (
        <span className="inline-block w-3.5 h-3.5 border-2 border-current border-t-transparent rounded-full animate-spin" />
      ) : Icon ? (
        <Icon className={size === 'sm' ? 'h-3.5 w-3.5' : 'h-4 w-4'} />
      ) : null}
      {children}
    </button>
  );
}
