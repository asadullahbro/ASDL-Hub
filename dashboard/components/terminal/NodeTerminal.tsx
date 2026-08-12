'use client';

import { useEffect, useRef, useCallback } from 'react';
import { Terminal } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';
import { WebLinksAddon } from 'xterm-addon-web-links';

const WS_BASE = (process.env.NEXT_PUBLIC_API_URL ?? 'https://hub.asdl.website')
  .replace('/api/v1', '')
  .replace('https://', 'wss://')
  .replace('http://', 'ws://');
  
interface NodeTerminalProps {
  nodeId: string;
  onClose: () => void;
}

export function NodeTerminal({ nodeId, onClose }: NodeTerminalProps) {
  const termRef = useRef<HTMLDivElement>(null);
  const termInstance = useRef<Terminal | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);

  const connect = useCallback(() => {
    const token = localStorage.getItem('token');
    if (!token || !termRef.current) return;

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'JetBrains Mono, Menlo, Monaco, Consolas, monospace',
      theme: {
        background: '#000000',
        foreground: '#f0f0f0',
        cursor: '#f59e0b',
        selectionBackground: '#f59e0b33',
        black: '#000000',
        brightBlack: '#444444',
        red: '#ef4444',
        brightRed: '#ef4444',
        green: '#22c55e',
        brightGreen: '#22c55e',
        yellow: '#f59e0b',
        brightYellow: '#f59e0b',
        blue: '#3b82f6',
        brightBlue: '#3b82f6',
        magenta: '#a855f7',
        brightMagenta: '#a855f7',
        cyan: '#06b6d4',
        brightCyan: '#06b6d4',
        white: '#f0f0f0',
        brightWhite: '#ffffff',
      },
    });

    const fit = new FitAddon();
    const links = new WebLinksAddon();

    term.loadAddon(fit);
    term.loadAddon(links);
    term.open(termRef.current);
    fit.fit();

    termInstance.current = term;
    fitAddon.current = fit;

    const ws = new WebSocket(
      `${WS_BASE}/api/v1/nodes/${nodeId}/terminal?token=${token}`
    );
    ws.binaryType = 'arraybuffer';
    wsRef.current = ws;

    ws.onopen = () => {
      term.write('\r\n\x1b[33mConnecting...\x1b[0m\r\n');
    };

    ws.onmessage = (e) => {
      if (e.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(e.data));
      } else {
        term.write(e.data);
      }
    };

    ws.onclose = () => {
      term.write('\r\n\x1b[31mConnection closed\x1b[0m\r\n');
    };

    ws.onerror = () => {
      term.write('\r\n\x1b[31mConnection error\x1b[0m\r\n');
    };

    // Send input to WebSocket
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'input', data }));
      }
    });

    // Handle resize
    const handleResize = () => {
      fit.fit();
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({
          type: 'resize',
          rows: term.rows,
          cols: term.cols,
        }));
      }
    };

    window.addEventListener('resize', handleResize);
    term.onResize(({ rows, cols }) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', rows, cols }));
      }
    });

    return () => {
      window.removeEventListener('resize', handleResize);
    };
  }, [nodeId]);

  useEffect(() => {
    const cleanup = connect();
    return () => {
      cleanup?.();
      wsRef.current?.close();
      termInstance.current?.dispose();
    };
  }, [connect]);

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-black">
      {/* Topbar */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-border bg-surface flex-shrink-0">
        <div className="flex items-center gap-2">
          <span className="w-2 h-2 rounded-full bg-status-green" />
          <span className="text-xs text-text-secondary font-mono">
            node terminal — {nodeId.slice(0, 8)}
          </span>
        </div>
        <button
          onClick={() => {
            wsRef.current?.close();
            onClose();
          }}
          className="text-text-muted hover:text-text-primary transition-colors text-xs font-mono px-2 py-1 border border-border rounded hover:bg-surface-hover"
        >
          ✕ close
        </button>
      </div>

      {/* Terminal */}
      <div ref={termRef} className="flex-1 p-2" />
    </div>
  );
}