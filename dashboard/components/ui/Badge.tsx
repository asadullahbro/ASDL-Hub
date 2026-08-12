import { ReactNode } from 'react';

interface BadgeProps {
  children: ReactNode;
  variant: 'online' | 'offline' | 'pending' | 'running' | 'completed' | 'failed';
  className?: string;
}

const variantMap = {
  online: 'badge-online',
  offline: 'badge-offline',
  pending: 'badge-pending',
  running: 'badge-running',
  completed: 'badge-completed',
  failed: 'badge-failed',
};

export function Badge({ children, variant, className = '' }: BadgeProps) {
  return <span className={`badge ${variantMap[variant]} ${className}`}>{children}</span>;
}
