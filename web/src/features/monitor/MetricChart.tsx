import { useId, useMemo, useState } from "react";

// A single-series time-series area chart, inline SVG (no chart library — the app
// keeps its dependency surface lean).
//
// Design (per the dataviz method): the data's job is change-over-time → a line.
// One series, so there is no legend (the title names it) and no categorical
// palette to validate; identity is carried by one accent hue drawn from the app's
// own tokens. Two measures of different scale are never put on one chart — each
// metric gets its own small multiple — so there is exactly one y-axis here. The
// line is 2px over a low-opacity fill anchored to the baseline, the grid is
// recessive, and a crosshair+tooltip is the default interaction on an SVG chart.

export interface ChartPoint {
  ts: string;
  value: number;
}

export function MetricChart({
  title,
  points,
  color,
  format,
  max,
}: {
  title: string;
  points: ChartPoint[];
  /** CSS color for the line/fill. A single series, so one hue. */
  color: string;
  /** Renders a value for the axis cap and the tooltip. */
  format: (v: number) => string;
  /** Fixed axis maximum (e.g. 100 for a percentage); auto from data when unset. */
  max?: number;
}) {
  const gradId = useId();
  const [hover, setHover] = useState<number | null>(null);

  // viewBox units; the SVG scales to its container via width/height 100%.
  const W = 320;
  const H = 96;
  const padL = 4;
  const padR = 4;
  const padT = 8;
  const padB = 4;

  const { linePath, areaPath, hi, xs, ys } = useMemo(() => {
    const n = points.length;
    const top = max ?? Math.max(1, ...points.map((p) => p.value));
    const innerW = W - padL - padR;
    const innerH = H - padT - padB;
    const x = (i: number) => padL + (n <= 1 ? innerW / 2 : (i / (n - 1)) * innerW);
    const y = (v: number) => padT + innerH - (Math.min(v, top) / top) * innerH;
    const xArr = points.map((_, i) => x(i));
    const yArr = points.map((p) => y(p.value));
    if (n === 0) return { linePath: "", areaPath: "", hi: top, xs: xArr, ys: yArr };
    const line = points.map((p, i) => `${i === 0 ? "M" : "L"}${x(i).toFixed(1)},${y(p.value).toFixed(1)}`).join(" ");
    const area = `${line} L${x(n - 1).toFixed(1)},${padT + innerH} L${x(0).toFixed(1)},${padT + innerH} Z`;
    return { linePath: line, areaPath: area, hi: top, xs: xArr, ys: yArr };
  }, [points, max]);

  const nearest = (clientX: number, rect: DOMRect) => {
    if (points.length === 0) return;
    const rel = ((clientX - rect.left) / rect.width) * W;
    let best = 0;
    let bestD = Infinity;
    for (let i = 0; i < xs.length; i++) {
      const d = Math.abs(xs[i] - rel);
      if (d < bestD) {
        bestD = d;
        best = i;
      }
    }
    setHover(best);
  };

  return (
    <div className="rounded-lg border border-border bg-panel p-4">
      <div className="mb-1 flex items-baseline justify-between">
        <h3 className="text-sm font-medium text-fg">{title}</h3>
        <span className="text-xs text-muted">
          {hover != null && points[hover]
            ? `${format(points[hover].value)} · ${new Date(points[hover].ts + "Z").toLocaleString()}`
            : points.length > 0
              ? format(points[points.length - 1].value)
              : "—"}
        </span>
      </div>
      {points.length === 0 ? (
        <p className="py-6 text-center text-xs text-muted">No history yet for this range.</p>
      ) : (
        <svg
          viewBox={`0 0 ${W} ${H}`}
          className="h-24 w-full"
          preserveAspectRatio="none"
          role="img"
          aria-label={`${title} over time`}
          onMouseMove={(e) => nearest(e.clientX, e.currentTarget.getBoundingClientRect())}
          onMouseLeave={() => setHover(null)}
        >
          <defs>
            <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity="0.28" />
              <stop offset="100%" stopColor={color} stopOpacity="0.02" />
            </linearGradient>
          </defs>
          {/* Recessive baseline. */}
          <line x1={padL} y1={H - padB} x2={W - padR} y2={H - padB} stroke="currentColor" strokeOpacity="0.12" strokeWidth="1" className="text-muted" />
          <path d={areaPath} fill={`url(#${gradId})`} />
          <path d={linePath} fill="none" stroke={color} strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" vectorEffect="non-scaling-stroke" />
          {hover != null && (
            <>
              <line x1={xs[hover]} y1={padT} x2={xs[hover]} y2={H - padB} stroke={color} strokeOpacity="0.4" strokeWidth="1" vectorEffect="non-scaling-stroke" />
              <circle cx={xs[hover]} cy={ys[hover]} r="3.5" fill={color} stroke="var(--panel, #11161f)" strokeWidth="1.5" />
            </>
          )}
        </svg>
      )}
      <div className="mt-1 flex justify-between text-[10px] text-muted">
        <span>0</span>
        <span>{format(hi)}</span>
      </div>
    </div>
  );
}
