import { runCommand } from './api.js';
import { navigate } from './router.js';
import { el, clear, copyToClipboard } from './util.js';
import { views } from './views/index.js';

const EXPORT_LABELS = { text: 'Download text', csv: 'Download CSV', json: 'Download JSON', html: 'Download HTML', manifest: 'Download manifest' };

function warningsBox(warnings) {
  if (!warnings?.length) return null;
  return el('ul', { class: 'warnings' }, warnings.map((w) => el('li', { text: w })));
}

function copyBlock(title, text) {
  const btn = el('button', { type: 'button', text: 'Copy' });
  btn.addEventListener('click', async () => {
    btn.textContent = (await copyToClipboard(text)) ? 'Copied' : 'Copy failed';
    setTimeout(() => { btn.textContent = 'Copy'; }, 1500);
  });
  return el('div', {}, el('div', { class: 'copyrow' }, el('span', { text: title }), btn), el('pre', { text }));
}

function shareLink(command, result, params) {
  const p = { ...params };
  if (command.group === 'forecast' && !p['random-seed'] && result.result?.run?.seed) p['random-seed'] = result.result.run.seed;
  const q = new URLSearchParams();
  for (const k of Object.keys(p).sort()) if (p[k] !== '') q.set(k, p[k]);
  q.set('run', '1');
  return `${location.origin}${location.pathname}#/${command.id}?${q}`;
}

/** renderResult draws a finished run into container. */
export function renderResult(container, command, result, params) {
  clear(container);
  const warn = warningsBox(result.warnings);
  if (warn) container.append(warn);

  const panels = [];
  const hasHTML = result.exports.includes('html');
  const outputBody = result.text
    ? el('pre', { text: result.text })
    : hasHTML ? reportFrame(result) : el('p', { class: 'muted', text: 'No output.' });

  const view = views[command.id];
  let viewNode = outputBody;
  if (view) {
    try {
      viewNode = view(result, command);
    } catch (e) {
      console.error(e);
      viewNode = el('div', { class: 'error-banner', text: `Could not draw this view (${e.message}); see the Output tab.` });
    }
  }
  panels.push({ key: 'result', label: 'Result', node: viewNode });
  if (view) panels.push({ key: 'output', label: 'Output', node: outputBody });
  if (hasHTML && view) panels.push({ key: 'report', label: 'Report', node: reportFrame(result) });
  else if (hasHTML && result.text) panels.push({ key: 'report', label: 'Report', node: reportFrame(result) });
  panels.push({ key: 'cli', label: 'CLI', node: el('div', {}, copyBlock('Command', result.cli), result.config ? copyBlock('Config (YAML)', result.config) : null) });

  const tabs = el('div', { class: 'tabs', role: 'tablist' });
  const body = el('div');
  const show = (key) => {
    for (const b of tabs.children) b.classList.toggle('active', b.dataset.key === key);
    clear(body).append(panels.find((p) => p.key === key).node);
  };
  for (const p of panels) tabs.append(el('button', { type: 'button', role: 'tab', dataset: { key: p.key }, text: p.label, onclick: () => show(p.key) }));
  container.append(tabs, body);
  show(panels[0].key);

  const downloads = el('div', { class: 'downloads' }, result.exports.map((f) =>
    el('a', { href: `/api/runs/${result.run_id}/export/${f}`, download: '', text: EXPORT_LABELS[f] ?? 'Download ' + f })));
  const share = el('button', { type: 'button', text: 'Share link' });
  share.addEventListener('click', async () => {
    share.textContent = (await copyToClipboard(shareLink(command, result, params))) ? 'Link copied' : 'Copy failed';
    setTimeout(() => { share.textContent = 'Share link'; }, 1500);
  });
  downloads.append(share);
  container.append(downloads);
}

function reportFrame(result) {
  return el('iframe', { class: 'report', title: 'Report', src: `/api/runs/${result.run_id}/export/html?inline=1` });
}

/** createRunner wires a form to /api/run and renders progress and results. */
export function createRunner(command, formApi, resultEl, ctx) {
  return async function submit() {
    formApi.clearErrors();
    const params = formApi.read();
    navigate(command.id, params, { replace: true });
    formApi.setBusy(true);
    clear(resultEl);

    const bar = el('progress', { max: 1, value: 0, hidden: true });
    const label = el('div', { class: 'muted' });
    const log = el('pre', { class: 'log', hidden: true });
    resultEl.append(el('div', { class: 'progress-area' }, bar, label, log));

    const result = await runCommand(command.id, params, (ev) => {
      if (ev.type === 'queued') label.textContent = 'Waiting for another run to finish…';
      else if (ev.type === 'progress') {
        bar.hidden = false;
        bar.max = ev.total;
        bar.value = ev.done;
        label.textContent = `${ev.done} / ${ev.total}`;
      } else if (ev.type === 'log') {
        log.hidden = false;
        log.append(document.createTextNode(ev.message + '\n'));
        log.scrollTop = log.scrollHeight;
      }
    });
    formApi.setBusy(false);

    if (result.type === 'error') {
      clear(resultEl);
      const warn = warningsBox(result.warnings);
      if (warn) resultEl.append(warn);
      formApi.showErrors(result.error);
      if (!result.error.fields?.length) {
        // Already shown in the form's banner.
      }
      ctx?.onError?.(result);
      return;
    }
    renderResult(resultEl, command, result, params);
  };
}
