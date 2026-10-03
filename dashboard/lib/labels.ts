// Names for job types, shared by every page that lists jobs.
const JOB_TYPES: Record<string, string> = {
  deploy: 'Deploy',
  command: 'Command',
  failover_stop: 'Remove old copy',
  failover_start: 'Failover',
  migrate_start: 'Move',
  agent_update: 'Agent update',
  container_stop: 'Stop container',
  container_start: 'Start container',
  container_restart: 'Restart container',
};

export function jobTypeLabel(type: string): string {
  return JOB_TYPES[type] ?? type.replace(/_/g, ' ').replace(/^\w/, c => c.toUpperCase());
}
