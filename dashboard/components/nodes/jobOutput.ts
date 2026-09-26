import { api } from '@/lib/api';

// Job logs from the agent are: a header, the output as it streamed, a
// "Completed at / Exit Code / Duration" footer, then the full output again
// in STDOUT: and STDERR: sections. Return that final copy once, or the
// streamed part if there is none.
export function jobOutputText(logs: string): string {
  const footer = logs.indexOf('\nCompleted at:');
  if (footer >= 0) {
    const tail = logs.slice(footer);
    const out = tail.match(/\nSTDOUT:\n([\s\S]*?)(?=\nSTDERR:\n|$)/);
    const err = tail.match(/\nSTDERR:\n([\s\S]*)$/);
    if (out || err) {
      return [out?.[1], err?.[1]].filter(Boolean).join('\n').trimEnd();
    }
  }
  const marker = '====================================\n\n';
  const start = logs.indexOf(marker);
  let body = start >= 0 ? logs.slice(start + marker.length) : logs;
  const end = body.indexOf('\n====================================\nCompleted at:');
  if (end >= 0) body = body.slice(0, end);
  return body.trimEnd();
}

// Waits for a node job to finish (nodes pick jobs up within ~5s) and returns
// its output.
export async function waitForJobOutput(jobId: string, timeoutMs = 60_000): Promise<{ ok: boolean; output: string }> {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    await new Promise(r => setTimeout(r, 1500));
    const job = await api.getJob(jobId);
    if (job.status === 'completed' || job.status === 'failed' || job.status === 'cancelled') {
      const { logs } = await api.getJobLogs(jobId).catch(() => ({ logs: job.logs ?? '' }));
      return { ok: job.status === 'completed', output: jobOutputText(logs) };
    }
  }
  throw new Error('The node did not answer in time. Is it online?');
}
