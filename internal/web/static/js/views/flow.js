import { el, clear } from '../util.js';
import { chartBox, histogram, lineChart } from '../charts.js';
import { table } from './table.js';

const dash = (v) => (v == null ? '–' : typeof v === 'number' ? (Number.isInteger(v) ? String(v) : v.toFixed(1)) : String(v));

function agingTable(items, sortable) {
  const cols = [
    ['identifier', 'Identifier'], ['title', 'Title'], ['age_days', 'Days', true], ['percentile', 'Percentile', true],
    ['multiplier', 'Multiplier', true], ['state', 'State'], ['started_at', 'Start date'], ['assignee', 'Assignee'],
  ];
  const holder = el('div');
  let sortKey = 'age_days';
  let desc = true;
  const draw = () => {
    const sorted = [...items].sort((a, b) => {
      const x = a[sortKey] ?? -Infinity;
      const y = b[sortKey] ?? -Infinity;
      return (x < y ? -1 : x > y ? 1 : 0) * (desc ? -1 : 1);
    });
    const head = el('tr', {}, cols.map(([key, label, num]) => el('th', {
      class: (num ? 'num ' : '') + (sortable ? 'sortable' : ''), text: label + (sortKey === key ? (desc ? ' ▼' : ' ▲') : ''),
      onclick: sortable ? () => { desc = sortKey === key ? !desc : true; sortKey = key; draw(); } : null,
    })));
    const rows = sorted.map((it) => el('tr', {},
      el('td', { text: it.identifier }),
      el('td', { title: it.title, text: it.title.length > 60 ? it.title.slice(0, 60) + '…' : it.title }),
      el('td', { class: 'num ' + it.age_class, text: it.age_days.toFixed(1) }),
      el('td', { class: 'num ' + it.age_class, text: it.percentile_label }),
      el('td', { class: 'num ' + it.age_class, text: it.multiplier_label }),
      el('td', { text: it.state }),
      el('td', { text: it.started_at }),
      el('td', { text: it.assignee })));
    clear(holder).append(el('div', { class: 'table-wrap' }, el('table', {}, el('thead', {}, head), el('tbody', {}, rows))));
  };
  draw();
  return holder;
}

/** aging renders the `aging` result. */
export function aging(ev) {
  const r = ev.result;
  const m = r.meta;
  const box = chartBox();
  const buckets = new Map();
  for (const ct of r.cycle_times) {
    const d = Math.ceil(ct);
    buckets.set(d, (buckets.get(d) ?? 0) + 1);
  }
  const bins = [...buckets.entries()].sort((a, b) => a[0] - b[0]).map(([value, count]) => ({ value, count }));
  const markers = m.has_threshold ? [{ value: Math.ceil(m.threshold), label: m.label }] : [];
  if (bins.length) histogram(box, bins, markers, { title: 'Cycle time distribution', xTitle: 'Days', yTitle: 'Completed issues' });
  return el('div', {},
    el('p', { text: `Cycle time distribution: ${m.completed_count} completed issues (${m.sample_start} to ${m.sample_end})  ·  ${m.label}: ${m.threshold.toFixed(1)} days` }),
    el('h3', { text: 'In progress' }),
    r.in_progress.length ? agingTable(r.in_progress, true) : el('p', { class: 'muted', text: 'Nothing in progress.' }),
    r.completed ? [el('h3', { text: 'Completed (the distribution sample)' }), agingTable(r.completed, true)] : null,
    bins.length ? box : null);
}

/** cfd renders the `cfd` result: the report frame plus the flow-health numbers. */
export function cfd(ev) {
  const health = ev.result.report.flow_health ?? {};
  const rows = Object.entries(health).map(([k, v]) => [k.replace(/_/g, ' '), String(v)]);
  return el('div', {},
    el('iframe', { class: 'report', title: 'Cumulative flow report', src: `/api/runs/${ev.run_id}/export/html?inline=1` }),
    el('h3', { text: 'Flow health' }),
    table([{ label: 'Metric' }, { label: 'Value', num: true }], rows));
}

const HISTORY_GROUPS = [
  ['Scope', [['total', 'Total'], ['completed', 'Completed'], ['remaining', 'Remaining']], 'Issues'],
  ['Throughput', [['throughput_7d', '7-day'], ['throughput_window', 'Window']], 'Issues per day'],
  ['Cycle time', [['cycle_time_p50', 'P50'], ['cycle_time_p85', 'P85']], 'Days'],
];

/** history renders the `history` result. */
export function history(ev) {
  const r = ev.result.report;
  const dates = r.series.map((row) => row.date);
  const charts = HISTORY_GROUPS.map(([title, keys, yTitle]) => {
    const box = chartBox();
    lineChart(box, keys.map(([k, name]) => ({ name, x: dates, y: r.series.map((row) => row[k]) })), { title, xTitle: 'Date', yTitle, showlegend: true });
    return box;
  });

  const columns = r.series.length ? Object.keys(r.series[0]) : [];
  const head = el('tr', {}, columns.map((c, i) => el('th', { class: i ? 'num' : '', text: c })));
  const body = r.series.map((row) => el('tr', {}, columns.map((c, i) => el('td', { class: i ? 'num' : '', text: dash(row[c]) }))));
  const wrap = el('div', { class: 'table-wrap', style: 'max-height: 60vh; overflow: auto;' }, el('table', {}, el('thead', {}, head), el('tbody', {}, body)));
  wrap.querySelectorAll('th').forEach((th) => { th.style.position = 'sticky'; th.style.top = '0'; th.style.background = 'var(--surface)'; });
  wrap.querySelectorAll('tr').forEach((tr) => { const c = tr.firstChild; if (c) { c.style.position = 'sticky'; c.style.left = '0'; c.style.background = 'var(--surface)'; } });

  return el('div', {},
    el('p', { text: `${r.total_issues} issue(s), ${r.skipped_issues} skipped (no created_at) · rolling window: ${r.window_days} days · starting ${ev.result.start}${ev.result.start_inferred ? ' (inferred)' : ''}` }),
    charts, wrap);
}

/** count renders the `count` result. */
export function count(ev) {
  const r = ev.result;
  const showMilestones = ev.flags?.some((f) => f.name === 'milestones' && f.value === 'true');
  const cols = [{ label: 'Project' }, ...(r.show_teams ? [{ label: 'Team' }] : []), { label: 'Issues', num: true }, { label: 'Milestones', num: true }, { label: 'Last updated' }];
  const cell = (p) => [p.name, ...(r.show_teams ? [p.team] : []), String(p.total), String(p.milestone_count), p.last_updated ?? '–'];
  const total = { class: 'total', cells: ['Total', ...(r.show_teams ? [''] : []), String(r.total), '', ''] };
  if (!showMilestones) return table(cols, [...r.projects.map(cell), total]);
  return el('div', {},
    r.projects.map((p) => el('details', {},
      el('summary', { text: `${p.name}${r.show_teams ? ` [${p.team}]` : ''} — ${p.total}` }),
      table([{ label: 'Milestone' }, { label: 'Issues', num: true }], p.milestones.map((m) => [m.name, String(m.count)])))),
    el('p', { class: 'total', text: `Total: ${r.total}` }));
}

const CHECK_LINKS = { history: 'history', cfd: 'cfd', aging: 'aging', count: 'count', sim: 'sim-days' };

/** check renders the `check` result as readiness cards. */
export function check(ev) {
  const r = ev.result;
  const cards = r.readiness.map((x) => el('div', { class: 'card ' + (x.excluded === 0 ? 'ok' : 'warn') },
    el('h4', {}, el('a', { href: `#/${CHECK_LINKS[x.command] ?? x.command}`, text: x.command })),
    el('div', { text: x.status })));
  const out = [el('p', { text: `Read ${r.issues} issues from ${r.input}` }), el('div', { class: 'cards' }, cards)];
  if (r.exclusions) {
    const e = r.exclusions;
    if (e.invalid) {
      out.push(el('div', { class: 'card bad', style: 'margin-top: 12px' }, el('h4', { text: `Exclusions: ${e.path}` }), el('div', { text: e.invalid })));
    } else {
      const lines = [`${e.global_entries} global entries, ${e.engineers.length} engineers` + (e.engineers.length ? ` (${e.engineers.map((x) => `${x.name}: ${x.entries}`).join(', ')})` : '')];
      lines.push(e.has_dates ? `span ${e.first} .. ${e.last} (${e.past} dates past, ${e.future} future, as of ${e.today})` : 'span none');
      for (const n of e.unmatched_names) lines.push(`${n}: no assignee named "${n}" in the input (fine if ${n} is given to sim -engineers)`);
      out.push(el('div', { class: 'card ' + (e.unmatched_names.length ? 'warn' : 'ok'), style: 'margin-top: 12px' },
        el('h4', { text: `Exclusions: ${e.path}` }), lines.map((l) => el('div', { text: l }))));
    }
  }
  return el('div', {}, out);
}
