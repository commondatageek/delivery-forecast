import { el } from './util.js';

// The embedded Plotly bundle (D14). Keep the path in one place.
const PLOTLY_SRC = '/vendor/plotly-2.35.2.min.js';

let loading = null;

/** loadPlotly injects the embedded Plotly once and resolves to window.Plotly, or null on failure. */
export function loadPlotly() {
  loading ??= new Promise((resolve) => {
    if (window.Plotly) { resolve(window.Plotly); return; }
    const s = el('script', { src: PLOTLY_SRC });
    s.addEventListener('load', () => resolve(window.Plotly ?? null));
    s.addEventListener('error', () => resolve(null));
    document.head.append(s);
  });
  return loading;
}

function palette() {
  const cs = getComputedStyle(document.documentElement);
  const v = (n) => cs.getPropertyValue(n).trim();
  return { text: v('--text'), muted: v('--muted'), grid: v('--chart-grid'), series: [v('--chart-1'), v('--chart-2'), v('--chart-3'), v('--danger')] };
}

function baseLayout(p, opts = {}) {
  return {
    title: opts.title ? { text: opts.title, font: { size: 14, color: p.text } } : undefined,
    paper_bgcolor: 'rgba(0,0,0,0)',
    plot_bgcolor: 'rgba(0,0,0,0)',
    font: { color: p.text, size: 12 },
    margin: { l: 56, r: 20, t: opts.title ? 40 : 16, b: 48 },
    xaxis: { title: opts.xTitle, gridcolor: p.grid, zerolinecolor: p.grid, ...(opts.xaxis ?? {}) },
    yaxis: { title: opts.yTitle, gridcolor: p.grid, zerolinecolor: p.grid, ...(opts.yaxis ?? {}) },
    legend: { orientation: 'h', y: -0.2 },
    showlegend: opts.showlegend ?? false,
    shapes: opts.shapes ?? [],
    annotations: opts.annotations ?? [],
    bargap: 0.05,
  };
}

/** chartBox returns an empty container a chart helper can draw into. */
export function chartBox() {
  return el('div', { class: 'chart' });
}

async function draw(box, data, layout) {
  const Plotly = await loadPlotly();
  if (!Plotly) {
    box.replaceChildren(el('p', { class: 'muted', text: 'Chart unavailable' }));
    return;
  }
  await Plotly.newPlot(box, data, layout, { responsive: true, displaylogo: false });
}

/** markerShapes returns a dashed vertical line and a label for each {value, label}. */
function markerShapes(p, markers) {
  const shapes = [];
  const annotations = [];
  markers.forEach((m, i) => {
    shapes.push({ type: 'line', x0: m.value, x1: m.value, yref: 'paper', y0: 0, y1: 1, line: { dash: 'dash', width: 1.5, color: p.series[(i % 2) + 1] } });
    annotations.push({ x: m.value, yref: 'paper', y: 1, text: m.label, showarrow: false, yanchor: 'bottom', font: { size: 11, color: p.text } });
  });
  return { shapes, annotations };
}

/** histogram draws [{value, count}] bins as bars, with a dashed marker per {value, label}. */
export function histogram(box, bins, markers = [], opts = {}) {
  const p = palette();
  const { shapes, annotations } = markerShapes(p, markers);
  return draw(box, [{
    type: 'bar', x: bins.map((b) => b.value), y: bins.map((b) => b.count),
    marker: { color: p.series[0] }, hovertemplate: '%{x}: %{y} simulations<extra></extra>',
  }], baseLayout(p, { ...opts, yTitle: opts.yTitle ?? 'Simulations', shapes, annotations }));
}

/**
 * lineChart draws series [{name, x, y, dash?}]. opts: {title, xTitle, yTitle,
 * shapes, annotations, showlegend}.
 */
export function lineChart(box, series, opts = {}) {
  const p = palette();
  const data = series.map((s, i) => ({
    type: 'scatter', mode: s.mode ?? 'lines', name: s.name, x: s.x, y: s.y,
    connectgaps: false, line: { color: s.color ?? p.series[i % p.series.length], dash: s.dash ?? 'solid', width: 2 },
  }));
  return draw(box, data, baseLayout(p, { showlegend: series.length > 1, ...opts }));
}

/** barChart draws one bar series with an optional horizontal reference line {y, label}. */
export function barChart(box, x, y, opts = {}) {
  const p = palette();
  const shapes = [];
  const annotations = [];
  if (opts.refY != null) {
    shapes.push({ type: 'line', xref: 'paper', x0: 0, x1: 1, y0: opts.refY, y1: opts.refY, line: { dash: 'dash', width: 1.5, color: p.series[1] } });
    annotations.push({ xref: 'paper', x: 1, y: opts.refY, text: opts.refLabel ?? '', showarrow: false, xanchor: 'right', yanchor: 'bottom', font: { size: 11, color: p.text } });
  }
  return draw(box, [{ type: 'bar', x, y, marker: { color: p.series[0] } }], baseLayout(p, { ...opts, shapes, annotations }));
}

/** vline returns shape/annotation entries for a vertical line at x (a date string or number). */
export function vline(x, label, color) {
  return {
    shape: { type: 'line', x0: x, x1: x, yref: 'paper', y0: 0, y1: 1, line: { dash: 'dot', width: 1.5, color } },
    annotation: { x, yref: 'paper', y: 1, text: label, showarrow: false, yanchor: 'bottom', font: { size: 11 } },
  };
}
