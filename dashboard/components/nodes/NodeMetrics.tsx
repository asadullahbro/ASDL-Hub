'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { LineChart } from 'lucide-react';
import { api } from '@/lib/api';
import type { NodeSample } from '@/types';
import { Card, EmptyState } from '@/components/ui/Card';

const RANGES = [
  { label: '6h', hours: 6 },
  { label: '24h', hours: 24 },
  { label: '7d', hours: 24 * 7 },
  { label: '14d', hours: 24 * 14 },
];

// A gap longer than this between two readings means the node was not reporting, so the line is broken there
// instead of drawing a straight run across the outage.
const GAP_MS = 5 * 60 * 1000;

interface Series {
  title: string;
  unit: string;
  pick: (s: NodeSample) => number;
  max?: number; // fixed top of the axis (percentages); otherwise it follows the data
  digits: number;
}

const SERIES: Series[] = [
  { title: 'CPU', unit: '%', pick: s => s.cpu_percent, max: 100, digits: 0 },
  { title: 'Memory', unit: '%', pick: s => s.memory_percent, max: 100, digits: 0 },
  { title: 'Load (1 min)', unit: '', pick: s => s.load_1, digits: 2 },
  { title: 'Ping to the Hub', unit: ' ms', pick: s => s.ping_ms, digits: 0 },
];

const W = 600;
const H = 120;
const PAD = 4;

function fmt(v: number, digits: number, unit: string) {
  return `${v.toFixed(digits)}${unit}`;
}

function clock(ms: number, hours: number) {
  const d = new Date(ms);
  return hours > 24
    ? d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
    : d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}

// One measure over time: a thin line on a recessive grid, with a crosshair and tooltip on hover and its
// latest, average and peak as text (the numbers are the accessible version of the picture).
function Chart({ series, samples, from, to, hours }: { series: Series; samples: NodeSample[]; from: number; to: number; hours: number }) {
  const box = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<number | null>(null);

  const pts = useMemo(() => samples.map(s => ({ t: new Date(s.at).getTime(), v: series.pick(s) })), [samples, series]);
  const top = series.max ?? Math.max(1, ...pts.map(p => p.v)) * 1.15;
  const x = (t: number) => PAD + ((t - from) / (to - from)) * (W - 2 * PAD);
  const y = (v: number) => H - PAD - (Math.min(v, top) / top) * (H - 2 * PAD);

  const path = useMemo(() => {
    let d = '';
    pts.forEach((p, i) => {
      const gap = i === 0 || p.t - pts[i - 1].t > GAP_MS;
      d += `${gap ? 'M' : 'L'}${x(p.t).toFixed(1)},${y(p.v).toFixed(1)}`;
    });
    return d;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pts, from, to, top]);

  const last = pts[pts.length - 1];
  const avg = pts.reduce((a, p) => a + p.v, 0) / pts.length;
  const peak = Math.max(...pts.map(p => p.v));

  const onMove = (e: React.MouseEvent) => {
    const r = box.current?.getBoundingClientRect();
    if (!r || r.width === 0) return;
    const t = from + ((e.clientX - r.left) / r.width) * (to - from);
    let best = 0;
    for (let i = 1; i < pts.length; i++) if (Math.abs(pts[i].t - t) < Math.abs(pts[best].t - t)) best = i;
    setHover(best);
  };

  const h = hover !== null ? pts[hover] : null;
  const hx = h ? ((x(h.t)) / W) * 100 : 0;

  return (
    <div>
      <div className="flex items-baseline justify-between gap-3 mb-1.5">
        <h3 className="text-xs font-medium text-text-primary">{series.title}</h3>
        <p className="text-xs text-text-secondary tabular-nums">
          now {fmt(last.v, series.digits, series.unit)} · avg {fmt(avg, series.digits, series.unit)} · peak {fmt(peak, series.digits, series.unit)}
        </p>
      </div>
      <div
        ref={box}
        className="relative"
        onMouseMove={onMove}
        onMouseLeave={() => setHover(null)}
        role="img"
        aria-label={`${series.title}: now ${fmt(last.v, series.digits, series.unit)}, average ${fmt(avg, series.digits, series.unit)}, peak ${fmt(peak, series.digits, series.unit)}`}
      >
        <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="w-full h-28 block">
          {[0, 0.5, 1].map(f => (
            <line key={f} x1={PAD} x2={W - PAD} y1={PAD + f * (H - 2 * PAD)} y2={PAD + f * (H - 2 * PAD)} className="stroke-border" strokeWidth={1} vectorEffect="non-scaling-stroke" />
          ))}
          <path d={path} fill="none" className="stroke-accent" strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" vectorEffect="non-scaling-stroke" />
          {h && <line x1={x(h.t)} x2={x(h.t)} y1={PAD} y2={H - PAD} className="stroke-border-strong" strokeWidth={1} vectorEffect="non-scaling-stroke" />}
        </svg>
        {h && (
          <>
            <span
              className="absolute h-2.5 w-2.5 -ml-[5px] -mt-[5px] rounded-full bg-accent ring-2 ring-surface pointer-events-none"
              style={{ left: `${hx}%`, top: `${(y(h.v) / H) * 100}%` }}
            />
            <div
              className="absolute -top-1 z-10 -translate-y-full rounded-md border border-border-strong bg-surface-hover px-2 py-1 text-xs text-text-primary shadow pointer-events-none whitespace-nowrap"
              style={{ left: `${Math.min(Math.max(hx, 12), 88)}%`, transform: 'translate(-50%, -100%)' }}
            >
              <span className="text-text-secondary">{clock(h.t, hours)}</span> <span className="tabular-nums">{fmt(h.v, series.digits, series.unit)}</span>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

// Load over time for one node. Hidden on a Hub too old to record readings.
export function NodeMetrics({ nodeId }: { nodeId: string }) {
  const [hours, setHours] = useState(24);
  const [samples, setSamples] = useState<NodeSample[] | null>(null);
  const [unavailable, setUnavailable] = useState(false);

  useEffect(() => {
    let live = true;
    const load = () =>
      api
        .getNodeSamples(nodeId, hours)
        .then(r => {
          if (live) {
            setSamples(r.samples);
            setUnavailable(false);
          }
        })
        .catch(() => {
          if (live) setUnavailable(true);
        });
    load();
    const t = setInterval(load, 60_000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [nodeId, hours]);

  if (unavailable && samples === null) return null;

  const to = Date.now();
  const from = to - hours * 3600 * 1000;

  return (
    <Card
      icon={LineChart}
      title="Load history"
      description="One reading a minute, kept for two weeks. Gaps are times the node was not reporting."
      actions={
        <div className="flex gap-1" role="group" aria-label="Time range">
          {RANGES.map(r => (
            <button
              key={r.hours}
              onClick={() => setHours(r.hours)}
              aria-pressed={hours === r.hours}
              className={`px-2.5 py-1 rounded-md text-xs border transition-colors ${
                hours === r.hours
                  ? 'border-accent text-text-primary bg-surface-hover'
                  : 'border-border text-text-secondary hover:text-text-primary hover:bg-surface-hover'
              }`}
            >
              {r.label}
            </button>
          ))}
        </div>
      }
    >
      {samples === null ? (
        <EmptyState>Loading…</EmptyState>
      ) : samples.length === 0 ? (
        <EmptyState>No readings in this range yet. They are collected once the Hub runs a version that records them.</EmptyState>
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-x-8 gap-y-6">
          {SERIES.map(s => (
            <Chart key={s.title} series={s} samples={samples} from={from} to={to} hours={hours} />
          ))}
        </div>
      )}
    </Card>
  );
}
