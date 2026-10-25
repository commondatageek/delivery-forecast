import { apiGet } from './api.js';
import { createDataBar } from './databar.js';
import { renderForm } from './form.js';
import { createRunner } from './result.js';
import { onRoute, parseHash, navigate } from './router.js';
import { el, clear } from './util.js';

const GROUPS = [['forecast', 'Forecast'], ['flow', 'Flow'], ['data', 'Data']];

async function boot() {
  const [{ commands }, version, defaults] = await Promise.all([
    apiGet('/api/commands'), apiGet('/api/version'), apiGet('/api/defaults'),
  ]);
  document.getElementById('version').textContent = 'forecast ' + version.version;

  const main = document.getElementById('main');
  const nav = document.getElementById('nav');
  const byId = Object.fromEntries(commands.map((c) => [c.id, c]));

  // Navigation: grouped links, plus a <select> shown at narrow widths.
  const select = el('select', { class: 'nav-select', 'aria-label': 'Command', onchange: (e) => navigate(e.target.value, {}) });
  for (const [group, title] of GROUPS) {
    const inGroup = commands.filter((c) => c.group === group);
    if (!inGroup.length) continue;
    nav.append(el('h3', { text: title }));
    for (const c of inGroup) {
      nav.append(el('a', { href: `#/${c.id}`, dataset: { id: c.id } }, c.title, el('small', { text: 'forecast ' + c.name })));
      select.append(el('option', { value: c.id, text: c.title }));
    }
  }
  nav.append(select);

  const databar = createDataBar(document.getElementById('databar'), defaults);
  let current = null; // {command, formApi, submit, resultEl}

  function render(route) {
    const command = byId[route.id];
    clear(main);
    if (!command) {
      main.append(el('h1', { text: 'Not found' }), el('p', { text: `No command "${route.id}".` }));
      return;
    }
    for (const a of nav.querySelectorAll('a')) a.classList.toggle('active', a.dataset.id === route.id);
    select.value = route.id;
    if (route.params.input) databar.setInput(route.params.input);

    const formEl = el('div');
    const resultEl = el('div', { id: 'result' });
    main.append(el('h1', { text: command.title }), el('p', { class: 'summary', text: command.summary }), formEl, resultEl);

    const build = (params) => {
      const formApi = renderForm(formEl, command, params, {
        summary: databar.getSummary(), defaults, getInput: databar.getInput,
        markInputInvalid: databar.markInvalid,
      });
      const submit = createRunner(command, formApi, resultEl, {});
      formApi.form.addEventListener('submit', (e) => { e.preventDefault(); submit(); });
      formApi.resetBtn.addEventListener('click', () => navigate(command.id, {}));
      current = { command, formApi, submit };
      return formApi;
    };
    build(route.params);
    if (route.run) current.submit();
  }

  // When the data source's summary changes, rebuild the form so its pickers
  // follow, keeping whatever the user has typed.
  databar.onChange(() => {
    if (!current || !byId[current.command.id]) return;
    const keep = current.formApi.read();
    delete keep.input;
    const formEl = main.children[2];
    if (!formEl) return;
    const resultEl = main.children[3];
    const formApi = renderForm(formEl, current.command, keep, {
      summary: databar.getSummary(), defaults, getInput: databar.getInput, markInputInvalid: databar.markInvalid,
    });
    const submit = createRunner(current.command, formApi, resultEl, {});
    formApi.form.addEventListener('submit', (e) => { e.preventDefault(); submit(); });
    formApi.resetBtn.addEventListener('click', () => navigate(current.command.id, {}));
    current = { command: current.command, formApi, submit };
  });

  onRoute(render);
  if (!location.hash) location.hash = '#/check';
  else render(parseHash());
}

boot().catch((e) => {
  document.getElementById('main').textContent = 'Failed to start: ' + e.message;
});
