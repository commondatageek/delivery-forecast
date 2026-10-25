// DOM helpers. Data (issue titles, project names, ...) can contain HTML
// characters, so nothing here ever assigns markup: elements are built with
// createElement and text goes in as text nodes.

/** el(tag, props, ...children): props are properties/attributes/listeners. */
export function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') node.className = v;
    else if (k === 'text') node.textContent = v;
    else if (k === 'dataset') Object.assign(node.dataset, v);
    else if (k.startsWith('on') && typeof v === 'function') node.addEventListener(k.slice(2), v);
    else if (k === 'list' || k === 'for' || k.includes('-') || k === 'inputmode') node.setAttribute(k, v === true ? '' : v);
    else if (k in node) node[k] = v;
    else node.setAttribute(k, v === true ? '' : v);
  }
  for (const c of children.flat(Infinity)) {
    if (c == null || c === false) continue;
    node.append(c.nodeType ? c : document.createTextNode(String(c)));
  }
  return node;
}

export function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
  return node;
}

export function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}

export async function copyToClipboard(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const ta = el('textarea', { value: text });
    document.body.append(ta);
    ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch { /* ignored */ }
    ta.remove();
    return ok;
  }
}

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

/** formatDate renders "YYYY-MM-DD" as "YYYY-MM-DD (Mon)"; the weekday is computed in UTC. */
export function formatDate(s) {
  if (!s) return '–';
  const d = new Date(s + 'T00:00:00Z');
  return Number.isNaN(d.getTime()) ? s : `${s} (${DAYS[d.getUTCDay()]})`;
}

export function storageGet(key) {
  try { return localStorage.getItem(key); } catch { return null; }
}
export function storageSet(key, value) {
  try { localStorage.setItem(key, value); } catch { /* storage unavailable */ }
}

export function debounce(fn, ms) {
  let t;
  return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); };
}
