import { api } from '@/lib/api';

// Waits for a node job to finish (nodes pick jobs up within ~5s) and returns
// its output without the runner's header.
export async function waitForJobOutput(jobId: string, timeoutMs = 60_000): Promise<{ ok: boolean; output: string }> {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    await new Promise(r => setTimeout(r, 1500));
    const job = await api.getJob(jobId);
    if (job.status === 'completed' || job.status === 'failed' || job.status === 'cancelled') {
      const { logs } = await api.getJobLogs(jobId).catch(() => ({ logs: job.logs ?? '' }));
      const marker = '====================================\n\n';
      const at = logs.indexOf(marker);
      return { ok: job.status === 'completed', output: (at >= 0 ? logs.slice(at + marker.length) : logs).trimEnd() };
    }
  }
  throw new Error('The node did not answer in time. Is it online?');
}
