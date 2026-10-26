import { el } from '../util.js';

/**
 * table builds a <table> inside a scroll container. columns: [{label, num?}];
 * rows: arrays of cell values (strings or Nodes) or {cells, class}.
 */
export function table(columns, rows) {
  const head = el('tr', {}, columns.map((c) => el('th', { class: c.num ? 'num' : '', text: c.label })));
  const body = rows.map((r) => {
    const cells = Array.isArray(r) ? r : r.cells;
    return el('tr', { class: Array.isArray(r) ? '' : r.class ?? '' }, cells.map((c, i) => el('td', { class: columns[i]?.num ? 'num' : '' }, c)));
  });
  return el('div', { class: 'table-wrap' }, el('table', {}, el('thead', {}, head), el('tbody', {}, body)));
}
