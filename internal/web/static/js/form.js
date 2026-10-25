import { apiGet, upload } from './api.js';
import { el, clear } from './util.js';

const START_CHOICES = ['today', 'tomorrow', 'yesterday', '-3 months', '-6 months'];
const END_CHOICES = ['now', ...START_CHOICES];
const SECTION_ORDER = ['data', 'basic', 'advanced'];

// The schema lists flags alphabetically; within a section, fields appear in
// this order (anything unlisted follows, alphabetically).
const FIELD_ORDER = [
  'project', 'milestone', 'teams', 'engineers', 'whole-team', 'typical-engineers',
  'items', 'days', 'target-start-date', 'target-end-date', 'confidence',
  'replay-start-date', 'start', 'end', 'sample-start', 'sample-end', 'percentile', 'window',
  'updated-since', 'milestones', 'show-completed', 'exclusions',
];
const rank = (name) => { const i = FIELD_ORDER.indexOf(name); return i < 0 ? FIELD_ORDER.length : i; };

let fileLists = {}; // kind → Promise of paths

function filesFor(kind) {
  fileLists[kind] ??= apiGet('/api/files?kind=' + kind)
    .then((r) => [...r.files, ...r.uploads].map((f) => f.path))
    .catch(() => []);
  return fileLists[kind];
}

let uid = 0;

/** toggleChips renders one toggle chip per value; clicking edits the comma list in input. */
function toggleChips(input, values) {
  const wrap = el('div', { class: 'chips' });
  const current = () => input.value.split(',').map((s) => s.trim()).filter(Boolean);
  const sync = () => {
    const have = new Set(current().map((s) => s.toUpperCase()));
    for (const chip of wrap.children) chip.classList.toggle('on', have.has(chip.dataset.value.toUpperCase()) || current().includes(chip.dataset.value));
  };
  for (const v of values) {
    wrap.append(el('span', {
      class: 'chip', dataset: { value: v }, text: v,
      onclick: () => {
        const cur = current();
        const i = cur.findIndex((c) => c.toUpperCase() === v.toUpperCase());
        if (i >= 0) cur.splice(i, 1); else cur.push(v);
        input.value = cur.join(',');
        sync();
      },
    }));
  }
  input.addEventListener('input', sync);
  sync();
  return wrap;
}

/**
 * renderForm builds the schema-driven form for command into container.
 * ctx: {summary, defaults, getInput}. It returns {form, read, showErrors,
 * clearErrors, setBusy}.
 */
export function renderForm(container, command, params, ctx) {
  clear(container);
  const summary = ctx.summary;
  const readers = []; // [{name, read()}]
  const fieldEls = {}; // name → {wrap, input, err}
  let projectInput = null;
  let milestoneList = null;

  const banner = el('div', { class: 'error-banner', hidden: true });

  function makeField(f) {
    const id = `f-${f.name}-${uid++}`;
    const initial = params[f.name] ?? '';
    const placeholder = f.default ? `default: ${f.default}` : '';
    const err = el('div', { class: 'field-error', hidden: true });
    let input;
    let extra = null;
    const wrap = el('div', { class: 'field' + (f.bool ? ' bool' : '') });

    if (f.bool) {
      input = el('input', { type: 'checkbox', id, checked: initial === 'true' || (initial === '' ? false : initial !== 'false' && f.default === 'true') });
      if (initial === '') input.checked = f.default === 'true';
      readers.push({ name: f.name, read: () => {
        if (input.checked && f.default === 'false') return 'true';
        if (!input.checked && f.default === 'true') return 'false';
        return '';
      } });
      wrap.append(el('label', { for: id }, input, f.label));
    } else {
      switch (f.kind) {
        case 'date-start':
        case 'date-end': {
          const listId = id + '-list';
          const choices = f.kind === 'date-end' ? END_CHOICES : START_CHOICES;
          input = el('input', { type: 'text', id, list: listId, placeholder, value: initial });
          const picker = el('input', { type: 'date', 'aria-label': f.label + ' (calendar)', onchange: (e) => { input.value = e.target.value; input.dispatchEvent(new Event('input')); } });
          extra = el('div', { class: 'pair' }, input, picker, el('datalist', { id: listId }, choices.map((c) => el('option', { value: c }))));
          break;
        }
        case 'project': {
          const listId = id + '-list';
          input = el('input', { type: 'text', id, list: listId, placeholder, value: initial });
          extra = el('div', {}, input, el('datalist', { id: listId }, (summary?.projects ?? []).map((p) => el('option', { value: p.name }))));
          projectInput = input;
          break;
        }
        case 'milestone': {
          const listId = id + '-list';
          input = el('input', { type: 'text', id, list: listId, placeholder, value: initial });
          milestoneList = el('datalist', { id: listId });
          extra = el('div', {}, input, milestoneList);
          break;
        }
        case 'team-list':
          input = el('input', { type: 'text', id, placeholder, value: initial });
          extra = el('div', {}, input, toggleChips(input, summary?.teams ?? []));
          break;
        case 'string':
          input = el('input', { type: 'text', id, placeholder, value: initial });
          if (command.id === 'linear-sync' && f.name === 'teams') {
            const slot = el('div', { class: 'chips' });
            extra = el('div', {}, input, slot);
            apiGet('/api/linear/teams').then((r) => slot.replaceWith(toggleChips(input, r.teams.map((t) => t.key))))
              .catch((e) => slot.replaceWith(el('small', { text: e.message })));
          }
          break;
        case 'assignee-list':
          input = el('input', { type: 'text', id, placeholder, value: initial });
          extra = el('div', {}, input, toggleChips(input, summary?.assignees ?? []));
          break;
        case 'exclusions-path':
        case 'db-path': {
          const listId = id + '-list';
          const list = el('datalist', { id: listId });
          const kind = f.kind === 'db-path' ? 'db' : 'exclusions';
          const start = initial || (f.name === 'exclusions' ? ctx.defaults?.exclusions ?? '' : '');
          input = el('input', { type: 'text', id, list: listId, placeholder, value: start });
          filesFor(kind).then((paths) => paths.forEach((p) => list.append(el('option', { value: p }))));
          const file = el('input', { type: 'file', hidden: true });
          file.addEventListener('change', async () => {
            if (!file.files[0]) return;
            try { input.value = (await upload(file.files[0])).path; } catch (e) { showFieldError(f.name, e.message); }
            file.value = '';
          });
          extra = el('div', { class: 'pair' }, input, list, el('button', { type: 'button', text: 'Upload…', onclick: () => file.click() }), file);
          break;
        }
        case 'choice': {
          input = el('select', { id }, el('option', { value: '', text: '(default)' }), (f.choices ?? []).map((c) => el('option', { value: c, text: c })));
          input.value = initial;
          break;
        }
        case 'int':
          input = el('input', { type: 'text', id, inputmode: 'numeric', placeholder, value: initial });
          break;
        default:
          input = el('input', { type: 'text', id, placeholder, value: initial });
      }
      if (!readers.some((r) => r.name === f.name)) readers.push({ name: f.name, read: () => input.value.trim() });
      wrap.append(el('label', { for: id, text: f.label }), extra ?? input);
    }
    if (f.usage) wrap.append(el('small', { text: f.usage }));
    wrap.append(err);
    fieldEls[f.name] = { wrap, input, err };
    return wrap;
  }

  const sections = { data: [], basic: [], advanced: [] };
  for (const f of command.fields) {
    if (f.kind === 'input-path') continue;
    (sections[f.section] ?? sections.basic).push(f);
  }
  for (const list of Object.values(sections)) list.sort((a, b) => rank(a.name) - rank(b.name));

  const form = el('form', { class: 'cmd', novalidate: true });
  form.append(banner);
  for (const s of SECTION_ORDER) {
    if (!sections[s].length) continue;
    const grid = el('div', { class: 'fields' }, sections[s].map(makeField));
    if (s === 'advanced') {
      const open = sections.advanced.some((f) => params[f.name]);
      form.append(el('details', { class: 'advanced', open }, el('summary', { text: 'Advanced' }), grid));
    } else {
      form.append(grid);
    }
  }

  const runBtn = el('button', { type: 'submit', class: 'primary', text: 'Run' });
  const resetBtn = el('button', { type: 'button', text: 'Reset' });
  const actions = el('div', { class: 'actions' }, runBtn, resetBtn, el('span', { class: 'spacer' }));
  form.append(actions);
  container.append(form);

  function updateMilestones() {
    if (!milestoneList) return;
    clear(milestoneList);
    const p = (summary?.projects ?? []).find((x) => x.name === projectInput?.value.trim());
    for (const m of p?.milestones ?? []) milestoneList.append(el('option', { value: m }));
  }
  projectInput?.addEventListener('input', updateMilestones);
  updateMilestones();

  function showFieldError(name, message) {
    const fe = fieldEls[name];
    if (!fe) return false;
    fe.input.classList.add('invalid');
    fe.input.setAttribute('aria-invalid', 'true');
    fe.err.textContent = message;
    fe.err.hidden = false;
    return true;
  }

  const api = {
    form, actions, resetBtn,
    /** read returns the non-empty field values as {name: string}. */
    read() {
      const out = {};
      for (const r of readers) {
        const v = r.read();
        if (v !== '') out[r.name] = v;
      }
      if (command.fields.some((f) => f.kind === 'input-path')) {
        const input = ctx.getInput();
        if (input) out.input = input;
      }
      return out;
    },
    clearErrors() {
      banner.hidden = true;
      for (const fe of Object.values(fieldEls)) {
        fe.input.classList.remove('invalid');
        fe.input.removeAttribute('aria-invalid');
        fe.err.hidden = true;
      }
      ctx.markInputInvalid?.(false);
    },
    showErrors(error) {
      const fields = error.fields ?? [];
      let placed = false;
      for (const name of fields) {
        if (name === 'input') { ctx.markInputInvalid?.(true); continue; }
        const rendered = fieldEls[name];
        if (rendered) {
          rendered.input.classList.add('invalid');
          rendered.input.setAttribute('aria-invalid', 'true');
          if (!placed) { showFieldError(name, error.message); placed = true; }
        }
      }
      if (!placed) {
        banner.textContent = error.message;
        banner.hidden = false;
      }
    },
    setBusy(busy) { runBtn.disabled = busy; },
  };
  return api;
}
