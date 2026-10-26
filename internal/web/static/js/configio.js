import { apiPost, ApiError } from './api.js';
import { navigate, setFlash } from './router.js';
import { el, copyToClipboard } from './util.js';

function noteLines(prefix, unknown, ignored) {
  const lines = [];
  if (unknown?.length) lines.push(`${prefix}: unknown keys skipped: ${unknown.join(', ')}`);
  if (ignored?.length) lines.push(`${prefix}: CLI-only flags skipped: ${ignored.join(', ')}`);
  return lines;
}

/**
 * attachConfigButtons adds "Import config…", "Export config" and "Copy CLI
 * command" to a form's action row.
 */
export function attachConfigButtons(formApi, command) {
  const file = el('input', { type: 'file', hidden: true, accept: '.yaml,.yml' });
  const importBtn = el('button', { type: 'button', text: 'Import config…', onclick: () => file.click() });
  const exportBtn = el('button', { type: 'button', text: 'Export config' });
  const copyBtn = el('button', { type: 'button', text: 'Copy CLI command' });

  file.addEventListener('change', async () => {
    const f = file.files[0];
    if (!f) return;
    try {
      const res = await apiPost('/api/config/parse', { command: command.id, yaml: await f.text() });
      setFlash(noteLines('Imported ' + f.name, res.unknown, res.ignored));
      navigate(command.id, res.params);
    } catch (e) {
      formApi.showErrors({ message: e.message, fields: [] });
    }
    file.value = '';
  });

  async function render() {
    formApi.clearErrors();
    try {
      return await apiPost('/api/config/render', { command: command.id, params: formApi.read() });
    } catch (e) {
      if (e instanceof ApiError) formApi.showErrors({ message: e.message, fields: e.fields });
      return null;
    }
  }

  exportBtn.addEventListener('click', async () => {
    const res = await render();
    if (!res) return;
    const url = URL.createObjectURL(new Blob([res.yaml], { type: 'text/yaml' }));
    const a = el('a', { href: url, download: `${command.id}.yaml` });
    document.body.append(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  });

  copyBtn.addEventListener('click', async () => {
    const res = await render();
    if (!res) return;
    copyBtn.textContent = (await copyToClipboard(res.cli)) ? 'Copied' : 'Copy failed';
    setTimeout(() => { copyBtn.textContent = 'Copy CLI command'; }, 1500);
  });

  formApi.actions.append(importBtn, exportBtn, copyBtn, file);
}

/** createPasteDialog builds the "Paste CLI command…" button and its dialog. */
export function createPasteDialog() {
  const area = el('textarea', { placeholder: 'forecast sim items -input x.csv -engineers 3 -days 30', 'aria-label': 'CLI command' });
  const err = el('div', { class: 'field-error', hidden: true });
  const dialog = el('dialog', {},
    el('h3', { text: 'Paste a CLI command' }), area, err,
    el('div', { class: 'actions' },
      el('button', { type: 'button', class: 'primary', text: 'Fill the form', onclick: async () => {
        err.hidden = true;
        try {
          const res = await apiPost('/api/cli/parse', { line: area.value });
          setFlash(noteLines('Pasted command', [], res.ignored));
          dialog.close();
          navigate(res.command, res.params);
        } catch (e) {
          err.textContent = e.message;
          err.hidden = false;
        }
      } }),
      el('button', { type: 'button', text: 'Cancel', onclick: () => dialog.close() })));
  document.body.append(dialog);
  return el('button', { type: 'button', text: 'Paste CLI command…', onclick: () => { area.value = ''; err.hidden = true; dialog.showModal(); } });
}
