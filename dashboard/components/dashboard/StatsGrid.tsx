'use client';

import { Server, ListTodo, CheckCircle, XCircle } from 'lucide-react';

interface StatsGridProps {
  stats: {
    nodes: number;
    onlineNodes: number;
    jobs: number;
    success: number;
    failed: number;
  };
}

export function StatsGrid({ stats }: StatsGridProps) {
  const items = [
    {
      label: 'Nodes',
      value: stats.nodes,
      sub: `${stats.onlineNodes} online`,
      icon: Server,
      color: 'text-text-primary',
    },
    {
      label: 'Jobs',
      value: stats.jobs,
      sub: 'Total jobs',
      icon: ListTodo,
      color: 'text-text-primary',
    },
    {
      label: 'Successful',
      value: stats.success,
      sub: `${stats.jobs ? Math.round(stats.success / stats.jobs * 100) : 0}% success rate`,
      icon: CheckCircle,
      color: 'text-status-green',
    },
    {
      label: 'Failed',
      value: stats.failed,
      sub: 'Jobs failed',
      icon: XCircle,
      color: 'text-status-red',
    },
  ];

  return (
    <div className="grid grid-cols-4 gap-4">
      {items.map((item) => {
        const Icon = item.icon;
        return (
          <div key={item.label} className="stat-card">
            <div className="flex items-start justify-between">
              <div>
                <div className="text-2xl font-semibold text-text-primary">
                  {item.value}
                </div>
                <div className="text-sm text-text-muted">{item.label}</div>
                <div className="mt-1 text-xs text-text-secondary">{item.sub}</div>
              </div>
              <Icon className={`h-5 w-5 ${item.color}`} />
            </div>
          </div>
        );
      })}
    </div>
  );
}
