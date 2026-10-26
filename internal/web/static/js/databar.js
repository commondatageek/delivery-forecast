import { apiGet, upload } from './api.js';
import { el, clear, debounce, storageGet, storageSet } from './util.js';

const STORE_KEY = 'forecast.input';

/** createDataBar builds the global data-source picker into container. */
export function createDataBar(container, defaults) {
  const listeners = [];
  let summary = null;
  let value = storageGet(STORE_KEY) || defaults.input || '';

  const input = el('input', { type: 'text', id: 'input', list: 'input-files', placeholder: 'Data source: path to a .db, .csv or .json file', 'aria-label': 'Data source', value });
  const datalist = el('datalist', { id: 'input-files' });
  const fileInput = el('input', { type: 'file', hidden: true, accept: '.csv,.json,.jsonl,.ndjson,.db,.sqlite,.sqlite3' });
  const chipSlot = el('span');
  const detail = el('ul', { class: 'readiness', hidden: true });
  const uploadBtn = el('button', { type: 'button', text: 'Upload…', onclick: () => fileInput.click() });
  container.append(el('div', { class: 'row' }, input, datalist, uploadBtn, fileInput, chipSlot), detail);

  apiGet('/api/files?kind=input').then((r) => {
    for (const f of [...r.files, ...r.uploads]) datalist.append(el('option', { value: f.path }));
  }).catch(() => {});

  function showChip(content, cls = '', onclick) {
    clear(chipSlot).append(el('span', { class: 'chip ' + cls, onclick }, content));
  }

  async function refresh() {
    const v = input.value.trim();
    input.classList.remove('invalid');
    detail.hidden = true;
    summary = null;
    if (!v) {
      clear(chipSlot);
      listeners.forEach((cb) => cb(null));
      return;
    }
    storageSet(STORE_KEY, v);
    showChip('loading…', '');
    try {
      summary = await apiGet('/api/datasets/summary?input=' + encodeURIComponent(v));
      const teams = summary.teams.length ? summary.teams.join(', ') : 'no teams';
      const range = summary.earliest_created ? ` · created ${summary.earliest_created}–updated ${summary.latest_updated}` : '';
      showChip(`${summary.issues} issues · ${teams}${range}`, '', () => { detail.hidden = !detail.hidden; });
      clear(detail).append(...summary.readiness.map((r) => el('li', {}, `${r.command}: ${r.status}`)));
    } catch (e) {
      showChip(e.message, 'error');
      input.classList.add('invalid');
    }
    listeners.forEach((cb) => cb(summary));
  }

  const debounced = debounce(refresh, 300);
  input.addEventListener('input', debounced);
  fileInput.addEventListener('change', async () => {
    const f = fileInput.files[0];
    if (!f) return;
    showChip('uploading…', '');
    try {
      const res = await upload(f);
      input.value = res.path;
      datalist.append(el('option', { value: res.path }));
      refresh();
    } catch (e) {
      showChip(e.message, 'error');
    }
    fileInput.value = '';
  });

  refresh();
  return {
    getInput: () => input.value.trim(),
    getSummary: () => summary,
    setInput(v) {
      if (v === input.value) return;
      input.value = v;
      refresh();
    },
    markInvalid(on) { input.classList.toggle('invalid', on); },
    onChange(cb) { listeners.push(cb); },
  };
}
