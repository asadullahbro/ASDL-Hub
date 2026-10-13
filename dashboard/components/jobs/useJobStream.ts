'use client';

import { useEffect, useState } from 'react';

// Same Hub that serves this dashboard (see the node terminal).
function wsBase() {
  if (process.env.NEXT_PUBLIC_API_URL) {
    return process.env.NEXT_PUBLIC_API_URL.replace('/api/v1', '').replace(/^http/, 'ws');
  }
  return window.location.origin.replace(/^http/, 'ws');
}

const MAX_CHARS = 1_000_000; // the browser keeps the tail only
const MAX_RETRIES = 5;

export interface JobStream {
  text: string;
  live: boolean; // connected and the job is still going
}

/**
 * A running job's output as it happens. Does nothing unless `active`; calls
 * onDone(status) when the job ends, so the caller can load the final record.
 */
export function useJobStream(jobId: string | null, active: boolean, onDone: (status: string) => void): JobStream {
  const [text, setText] = useState('');
  const [live, setLive] = useState(false);

  useEffect(() => {
    if (!jobId || !active) return;
    let token: string | null = null;
    try {
      token = localStorage.getItem('token');
    } catch {
      /* no storage: no live view */
    }
    if (!token) return;

    let socket: WebSocket | null = null;
    let finished = false;
    let retries = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    setText('');

    const connect = () => {
      socket = new WebSocket(`${wsBase()}/api/v1/jobs/${jobId}/stream?token=${encodeURIComponent(token!)}`);
      socket.onopen = () => {
        retries = 0;
        setLive(true);
      };
      socket.onmessage = ev => {
        let msg: { type: string; data?: string; status?: string };
        try {
          msg = JSON.parse(ev.data);
        } catch {
          return;
        }
        if (msg.type === 'snapshot') setText((msg.data ?? '').slice(-MAX_CHARS));
        else if (msg.type === 'chunk') setText(t => (t + (msg.data ?? '')).slice(-MAX_CHARS));
        else if (msg.type === 'done') {
          finished = true;
          setLive(false);
          onDone(msg.status ?? '');
        }
      };
      socket.onclose = () => {
        setLive(false);
        if (finished || retries >= MAX_RETRIES) return;
        retries += 1;
        timer = setTimeout(connect, Math.min(1000 * retries, 5000));
      };
    };
    connect();

    return () => {
      finished = true; // a closed modal must not reconnect
      clearTimeout(timer);
      socket?.close();
      setLive(false);
    };
    // onDone is read through the latest closure on purpose; reconnecting for it would restart the stream
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jobId, active]);

  return { text, live };
}
