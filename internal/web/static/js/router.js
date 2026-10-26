// Hash routing: #/<command-id>?<params>. "run=1" is a UI-only key meaning
// "submit on arrival"; it is never sent to the API.

export function parseHash(hash = location.hash) {
  const m = /^#\/([^?]*)(?:\?(.*))?$/.exec(hash);
  if (!m || !m[1]) return { id: 'check', params: {}, run: false };
  const params = {};
  for (const [k, v] of new URLSearchParams(m[2] || '')) params[k] = v;
  const run = params.run === '1';
  delete params.run;
  return { id: m[1], params, run };
}

export function buildHash(id, params, { run = false } = {}) {
  const q = new URLSearchParams();
  for (const k of Object.keys(params).sort()) {
    if (params[k] !== '' && params[k] != null) q.set(k, params[k]);
  }
  if (run) q.set('run', '1');
  const s = q.toString();
  return `#/${id}${s ? '?' + s : ''}`;
}

/** navigate changes the route. With replace the page is not re-rendered. */
export function navigate(id, params, { replace = false, run = false } = {}) {
  const hash = buildHash(id, params, { run });
  if (replace) history.replaceState(null, '', hash);
  else location.hash = hash;
}

export function onRoute(cb) {
  window.addEventListener('hashchange', () => cb(parseHash()));
}

let flash = null;

/** setFlash stores a one-shot note shown on the next page render. */
export function setFlash(lines) { flash = lines; }

/** takeFlash returns and clears the pending note. */
export function takeFlash() {
  const f = flash;
  flash = null;
  return f;
}
