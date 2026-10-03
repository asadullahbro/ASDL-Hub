'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, ClipboardPaste, Minus, Plus, RotateCw, SquareTerminal, X } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { Terminal } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';
import { WebLinksAddon } from 'xterm-addon-web-links';

// The terminal talks to the Hub that serves this dashboard.
function wsBase() {
  if (process.env.NEXT_PUBLIC_API_URL) {
    return process.env.NEXT_PUBLIC_API_URL.replace('/api/v1', '').replace(/^http/, 'ws');
  }
  return window.location.origin.replace(/^http/, 'ws');
}

type Status = 'connecting' | 'opening' | 'connected' | 'closed';

const FONT_KEY = 'terminal-font-size';
const THEME = {
  background: '#000000',
  foreground: '#f0f0f0',
  cursor: '#f59e0b',
  cursorAccent: '#000000',
  selectionBackground: '#f59e0b40',
  black: '#000000',
  brightBlack: '#5f5f5f',
  red: '#ef4444',
  brightRed: '#f87171',
  green: '#22c55e',
  brightGreen: '#4ade80',
  yellow: '#eab308',
  brightYellow: '#facc15',
  blue: '#3b82f6',
  brightBlue: '#60a5fa',
  magenta: '#a855f7',
  brightMagenta: '#c084fc',
  cyan: '#06b6d4',
  brightCyan: '#22d3ee',
  white: '#e5e5e5',
  brightWhite: '#ffffff',
};

// Keys phones don't have, sent as the bytes a terminal expects.
const KEYS: { label: string; icon?: LucideIcon; seq: string; title: string }[] = [
  { label: 'Esc', seq: '\x1b', title: 'Escape' },
  { label: 'Tab', seq: '\t', title: 'Tab (completion)' },
  { label: '^C', seq: '\x03', title: 'Ctrl+C (stop)' },
  { label: '', icon: ArrowUp, seq: '\x1b[A', title: 'Up (history)' },
  { label: '', icon: ArrowDown, seq: '\x1b[B', title: 'Down' },
  { label: '', icon: ArrowLeft, seq: '\x1b[D', title: 'Left' },
  { label: '', icon: ArrowRight, seq: '\x1b[C', title: 'Right' },
  { label: '|', seq: '|', title: 'Pipe' },
  { label: '/', seq: '/', title: 'Slash' },
  { label: '-', seq: '-', title: 'Dash' },
  { label: '~', seq: '~', title: 'Home' },
  { label: '^D', seq: '\x04', title: 'Ctrl+D (exit)' },
  { label: '^L', seq: '\x0c', title: 'Ctrl+L (clear)' },
];

interface NodeTerminalProps {
  nodeId: string;
  nodeName?: string;
  onClose: () => void;
}

export function NodeTerminal({ nodeId, nodeName, onClose }: NodeTerminalProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<HTMLDivElement>(null);
  const term = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);
  const ws = useRef<WebSocket | null>(null);
  const ctrl = useRef(false);
  const [ctrlOn, setCtrlOn] = useState(false);
  const [status, setStatus] = useState<Status>('connecting');
  const [fontSize, setFontSize] = useState(13);
  const [attempt, setAttempt] = useState(0);

  const send = useCallback((data: string) => {
    if (ws.current?.readyState === WebSocket.OPEN) ws.current.send(JSON.stringify({ type: 'input', data }));
  }, []);

  const sendSize = useCallback(() => {
    const t = term.current;
    if (!t || !fit.current) return;
    try {
      fit.current.fit();
    } catch {
      return;
    }
    if (ws.current?.readyState === WebSocket.OPEN) ws.current.send(JSON.stringify({ type: 'resize', rows: t.rows, cols: t.cols }));
  }, []);

  // The terminal itself, created once.
  useEffect(() => {
    let size = window.innerWidth < 640 ? 12 : 13;
    try {
      const saved = Number(localStorage.getItem(FONT_KEY));
      if (saved >= 9 && saved <= 24) size = saved;
    } catch {}
    setFontSize(size);

    const t = new Terminal({
      cursorBlink: true,
      fontSize: size,
      fontFamily: 'JetBrains Mono, ui-monospace, Menlo, Monaco, Consolas, monospace',
      lineHeight: 1.15,
      scrollback: 5000,
      theme: THEME,
      allowProposedApi: true,
    });
    const f = new FitAddon();
    t.loadAddon(f);
    t.loadAddon(new WebLinksAddon());
    t.open(termRef.current!);
    term.current = t;
    fit.current = f;

    // Ctrl from the key bar applies to the next key typed.
    const sub = t.onData(data => {
      if (ctrl.current && data.length === 1) {
        const c = data.toUpperCase().charCodeAt(0);
        if (c >= 64 && c <= 95) data = String.fromCharCode(c - 64);
        ctrl.current = false;
        setCtrlOn(false);
      }
      send(data);
    });

    const ro = new ResizeObserver(() => sendSize());
    ro.observe(termRef.current!);
    document.body.style.overflow = 'hidden';
    return () => {
      sub.dispose();
      ro.disconnect();
      t.dispose();
      document.body.style.overflow = '';
    };
  }, [send, sendSize]);

  // The connection, again on each reconnect.
  useEffect(() => {
    const t = term.current;
    const token = (() => {
      try {
        return localStorage.getItem('token');
      } catch {
        return null;
      }
    })();
    if (!t || !token) return;
    setStatus('connecting');
    const socket = new WebSocket(`${wsBase()}/api/v1/nodes/${nodeId}/terminal?token=${encodeURIComponent(token)}`);
    socket.binaryType = 'arraybuffer';
    ws.current = socket;
    socket.onopen = () => {
      // The node still has to start a shell; "connected" once it answers.
      setStatus('opening');
      sendSize();
      t.focus();
    };
    socket.onmessage = e => {
      setStatus('connected');
      t.write(e.data instanceof ArrayBuffer ? new Uint8Array(e.data) : e.data);
    };
    socket.onclose = () => {
      if (ws.current !== socket) return;
      setStatus('closed');
      t.write('\r\n\x1b[90m— disconnected —\x1b[0m\r\n');
    };
    return () => {
      ws.current = null;
      socket.close();
    };
  }, [nodeId, attempt, sendSize]);

  // On phones the keyboard shrinks the visible area; keep everything in it.
  useEffect(() => {
    const vv = window.visualViewport;
    if (!vv) return;
    const onResize = () => {
      if (rootRef.current) {
        rootRef.current.style.height = `${vv.height}px`;
        rootRef.current.style.top = `${vv.offsetTop}px`;
      }
    };
    onResize();
    vv.addEventListener('resize', onResize);
    vv.addEventListener('scroll', onResize);
    return () => {
      vv.removeEventListener('resize', onResize);
      vv.removeEventListener('scroll', onResize);
    };
  }, []);

  const changeFont = (delta: number) => {
    const next = Math.min(24, Math.max(9, fontSize + delta));
    setFontSize(next);
    if (term.current) term.current.options.fontSize = next;
    try {
      localStorage.setItem(FONT_KEY, String(next));
    } catch {}
    requestAnimationFrame(sendSize);
  };

  const paste = async () => {
    try {
      const text = await navigator.clipboard.readText();
      if (text) send(text);
    } catch {
      term.current?.write('\r\n\x1b[90m(the browser didn\'t allow reading the clipboard; long-press in the terminal to paste)\x1b[0m\r\n');
    }
    term.current?.focus();
  };

  const close = () => {
    ws.current?.close();
    onClose();
  };

  const dot = { connecting: 'bg-status-yellow animate-pulse', opening: 'bg-status-yellow animate-pulse', connected: 'bg-status-green', closed: 'bg-status-red' }[status];
  const label = { connecting: 'Connecting…', opening: 'Opening a shell…', connected: 'Connected', closed: 'Disconnected' }[status];

  return (
    <div ref={rootRef} className="fixed inset-x-0 top-0 h-[100dvh] z-50 flex flex-col bg-black">
      <header className="flex items-center gap-2 h-11 px-2 sm:px-3 border-b border-border bg-surface flex-shrink-0">
        <button onClick={close} aria-label="Close terminal" className="h-8 w-8 flex items-center justify-center rounded-md text-text-secondary hover:text-text-primary hover:bg-surface-hover">
          <X className="h-4 w-4" />
        </button>
        <SquareTerminal className="h-4 w-4 text-text-secondary hidden sm:block" />
        <div className="min-w-0 flex-1">
          <div className="text-sm text-text-primary truncate leading-tight">{nodeName ?? nodeId.slice(0, 8)}</div>
          <div className="flex items-center gap-1.5 text-[11px] text-text-secondary leading-tight">
            <span className={`h-1.5 w-1.5 rounded-full ${dot}`} />
            {label}
          </div>
        </div>
        {status === 'closed' && (
          <ToolButton icon={RotateCw} title="Reconnect" onClick={() => setAttempt(a => a + 1)} label="Reconnect" />
        )}
        <ToolButton icon={ClipboardPaste} title="Paste" onClick={paste} />
        <div className="flex items-center rounded-md border border-border">
          <ToolButton icon={Minus} title="Smaller text" onClick={() => changeFont(-1)} bare />
          <span className="text-[11px] text-text-secondary w-6 text-center tabular-nums">{fontSize}</span>
          <ToolButton icon={Plus} title="Larger text" onClick={() => changeFont(1)} bare />
        </div>
      </header>

      <div className="relative flex-1 min-h-0 px-1.5 pt-1.5 sm:px-3 sm:pt-2" onClick={() => term.current?.focus()}>
        <div ref={termRef} className="h-full w-full" />
        {(status === 'connecting' || status === 'opening') && (
          <div className="absolute inset-0 flex items-center justify-center pointer-events-none">
            <div className="flex items-center gap-2 text-xs text-text-secondary">
              <span className="h-3.5 w-3.5 rounded-full border-2 border-accent border-t-transparent animate-spin" />
              {status === 'connecting' ? 'Connecting to the Hub…' : `Opening a shell on ${nodeName ?? 'the node'}…`}
            </div>
          </div>
        )}
      </div>

      {/* Keys phones lack. Buttons don't take focus, so the keyboard stays open. */}
      <div className="flex-shrink-0 border-t border-border bg-surface px-1.5 py-1.5 overflow-x-auto [scrollbar-width:none] sm:hidden">
        <div className="flex gap-1 w-max">
          <KeyButton
            active={ctrlOn}
            title="Ctrl: applies to the next key"
            onPress={() => {
              ctrl.current = !ctrl.current;
              setCtrlOn(ctrl.current);
              term.current?.focus();
            }}
          >
            Ctrl
          </KeyButton>
          {KEYS.map(k => (
            <KeyButton
              key={k.title}
              title={k.title}
              onPress={() => {
                send(k.seq);
                term.current?.focus();
              }}
            >
              {k.icon ? <k.icon className="h-3.5 w-3.5" /> : k.label}
            </KeyButton>
          ))}
        </div>
      </div>
    </div>
  );
}

function ToolButton({ icon: Icon, title, onClick, label }: { icon: LucideIcon; title: string; onClick: () => void; label?: string; bare?: boolean }) {
  return (
    <button
      onClick={onClick}
      title={title}
      aria-label={title}
      className={`h-8 ${label ? 'px-2.5 gap-1.5' : 'w-8'} flex items-center justify-center rounded-md text-xs text-text-secondary hover:text-text-primary hover:bg-surface-hover`}
    >
      <Icon className="h-3.5 w-3.5" />
      {label && <span className="hidden sm:inline">{label}</span>}
    </button>
  );
}

function KeyButton({ children, onPress, active, title }: { children: React.ReactNode; onPress: () => void; active?: boolean; title: string }) {
  return (
    <button
      title={title}
      aria-label={title}
      onPointerDown={e => {
        // Keep focus (and the phone keyboard) on the terminal.
        e.preventDefault();
        onPress();
      }}
      className={`h-9 min-w-10 px-2.5 flex items-center justify-center rounded-md border text-xs font-mono select-none active:bg-surface-hover ${
        active ? 'border-accent bg-accent/15 text-accent' : 'border-border bg-background text-text-primary'
      }`}
    >
      {children}
    </button>
  );
}
