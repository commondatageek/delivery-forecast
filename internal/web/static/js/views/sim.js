import { el, formatDate } from '../util.js';
import { chartBox, histogram, lineChart, barChart, vline } from '../charts.js';
import { table } from './table.js';

const pct = (n) => `${n}%`;

/** simItems renders the `sim items` result. */
export function simItems(ev) {
  const r = ev.result;
  const box = chartBox();
  histogram(box, r.histogram, r.rows.map((row) => ({ value: row.items, label: pct(row.confidence) })), {
    title: 'Items completed across simulations', xTitle: 'Items completed',
  });
  return el('div', {},
    el('p', { text: `${r.run.mode_label}, ${r.target_start} to ${r.target_end} (${r.effective_days} days)` }),
    table([{ label: 'Confidence' }, { label: 'Items' }], r.rows.map((row) => [pct(row.confidence), `at least ${row.items}`])),
    box);
}

/** simDays renders the `sim days` result: single mode or the grouped trajectory. */
export function simDays(ev) {
  const r = ev.result;
  if (r.groups) return trajectory(r);
  const box = chartBox();
  histogram(box, r.histogram, r.single.map((row) => ({ value: row.days, label: pct(row.confidence) })), {
    title: 'Days to finish across simulations', xTitle: 'Days',
  });
  return el('div', {},
    el('p', { text: `${r.run.mode_label}, ${r.items[0]} items -> how many days?` }),
    table([{ label: 'Confidence' }, { label: 'Days', num: true }, { label: 'Date' }],
      r.single.map((row) => [pct(row.confidence), String(row.days), formatDate(row.date)])),
    box);
}

function trajectory(r) {
  const head1 = el('tr', {}, el('th'), el('th'), r.confidences.map((c) => el('th', { colspan: 2, text: pct(c) })));
  const head2 = el('tr', {}, el('th', { text: 'Group' }), el('th', { class: 'num', text: 'Items' }),
    r.confidences.map(() => [el('th', { class: 'num', text: 'Days' }), el('th', { text: 'Date' })]));
  const rowFor = (label, g, cls) => el('tr', { class: cls }, el('td', { text: label }), el('td', { class: 'num', text: String(g.items) }),
    g.cells.map((c) => [el('td', { class: 'num', text: String(c.marginal_days) }), el('td', { text: formatDate(c.date) })]));
  const rows = r.groups.map((g, i) => rowFor(`Group ${i + 1}`, g, ''));
  rows.push(rowFor('Total', r.total, 'total'));

  // Cumulative items against cumulative date, one line per confidence level.
  const series = r.confidences.map((c, ci) => {
    let cumItems = 0;
    const x = [];
    const y = [];
    r.groups.forEach((g) => {
      cumItems += g.items;
      x.push(g.cells[ci].date);
      y.push(cumItems);
    });
    return { name: pct(c), x, y, mode: 'lines+markers' };
  });
  const box = chartBox();
  lineChart(box, series, { title: 'Cumulative items by date', xTitle: 'Date', yTitle: 'Items' });
  return el('div', {},
    el('p', { text: `${r.run.mode_label}, starting ${r.target_start} -> grouped trajectory` }),
    el('div', { class: 'table-wrap' }, el('table', {}, el('thead', {}, head1, head2), el('tbody', {}, rows))),
    box);
}

/** simProbability renders the `sim probability` result. */
export function simProbability(ev) {
  const r = ev.result;
  const header = el('p', { text: `${r.run.mode_label}, ${r.target_start} to ${r.target_end} (${r.effective_days} days)` });
  if (r.probability != null) {
    return el('div', {}, header,
      el('div', { class: 'big', text: `${r.probability.toFixed(1)}%` }),
      el('p', { text: `You can commit to ${r.items} items here at ~${r.probability.toFixed(0)}% confidence.` }));
  }
  const box = chartBox();
  barChart(box, r.table.map((row) => row.items), r.table.map((row) => row.probability), {
    title: 'Probability of completing at least N items', xTitle: 'Items', yTitle: 'Probability (%)', refY: 85, refLabel: '85%',
    yaxis: { range: [0, 100] },
  });
  return el('div', {}, header,
    table([{ label: 'Items', num: true }, { label: 'Probability', num: true }], r.table.map((row) => [String(row.items), row.probability == null ? '–' : `${row.probability.toFixed(1)}%`])),
    box);
}

/** simBacktest renders the `sim backtest` result. */
export function simBacktest(ev) {
  const r = ev.result;
  const probBox = chartBox();
  const flowBox = chartBox();
  const past = r.rows.filter((x) => !x.projected);
  const projected = r.rows.filter((x, i) => x.projected || (i > 0 && r.rows[i - 1].projected));
  // The projected segment starts at the last past point so the lines join.
  const joined = past.length ? [past[past.length - 1], ...r.rows.filter((x) => x.projected)] : projected;
  const today = vline(r.today, 'today', getComputedStyle(document.documentElement).getPropertyValue('--muted').trim());
  const common = { shapes: [today.shape], annotations: [today.annotation], xTitle: 'Date' };
  lineChart(probBox, [
    { name: 'Probability', x: past.map((x) => x.date), y: past.map((x) => x.probability) },
    { name: 'Projected', x: joined.map((x) => x.date), y: joined.map((x) => x.probability), dash: 'dash', color: undefined },
  ], { ...common, title: 'Probability of meeting the target date', yTitle: 'Probability (%)', yaxis: { range: [0, 100] } });
  lineChart(flowBox, [
    { name: 'Remaining', x: r.rows.map((x) => x.date), y: r.rows.map((x) => x.remaining) },
    { name: 'Completed', x: r.rows.map((x) => x.date), y: r.rows.map((x) => x.completed) },
  ], { ...common, title: 'Remaining vs completed', yTitle: 'Issues' });

  const rows = [];
  let dividerDone = false;
  for (const x of r.rows) {
    if (x.projected && !dividerDone) {
      dividerDone = true;
      rows.push({ class: 'divider', cells: [`today: ${r.today} — rows below are projected`, '', '', ''] });
    }
    rows.push({ class: x.projected ? 'projected' : '', cells: [x.date, String(x.completed), String(x.remaining), x.probability == null ? '–' : `${x.probability.toFixed(1)}%`] });
  }
  return el('div', {},
    el('p', { text: `Backtest: ${r.scope} (${r.issue_count} issues, ${r.run.mode_label})` }),
    el('p', { class: 'muted', text: `Replay start: ${r.replay_start}${r.replay_start_inferred ? ' (inferred)' : ''}` }),
    probBox, flowBox,
    table([{ label: 'Date' }, { label: 'Completed', num: true }, { label: 'Remaining', num: true }, { label: 'Probability', num: true }], rows));
}
