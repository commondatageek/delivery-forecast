const CLIENT = { 'X-Forecast-Client': 'web' };

export class ApiError extends Error {
  constructor(status, body) {
    super(body?.error?.message ?? `HTTP ${status}`);
    this.status = status;
    this.fields = body?.error?.fields ?? [];
    this.warnings = body?.warnings ?? [];
  }
}

async function parse(resp) {
  let body = null;
  try { body = await resp.json(); } catch { /* not JSON */ }
  if (!resp.ok) throw new ApiError(resp.status, body);
  return body;
}

export async function apiGet(path) {
  return parse(await fetch(path));
}

export async function apiPost(path, body) {
  return parse(await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...CLIENT },
    body: JSON.stringify(body),
  }));
}

export async function upload(file) {
  const fd = new FormData();
  fd.append('file', file);
  return parse(await fetch('/api/uploads', { method: 'POST', headers: CLIENT, body: fd }));
}

/**
 * runCommand POSTs /api/run/{id} and reads the NDJSON stream. It resolves with
 * the terminal event ({type: "result", ...} or {type: "error", ...}); a 400
 * resolves as {type: "error", error, warnings}. onEvent receives the
 * queued/progress/log events.
 */
export async function runCommand(id, params, onEvent) {
  let resp;
  try {
    resp = await fetch(`/api/run/${id}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...CLIENT },
      body: JSON.stringify({ params }),
    });
  } catch (e) {
    return { type: 'error', error: { message: `request failed: ${e.message}`, fields: [] }, warnings: [] };
  }
  if (!resp.ok) {
    let body = null;
    try { body = await resp.json(); } catch { /* ignore */ }
    return { type: 'error', error: body?.error ?? { message: `HTTP ${resp.status}`, fields: [] }, warnings: body?.warnings ?? [] };
  }
  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  let terminal = null;
  const handle = (line) => {
    if (!line.trim()) return;
    const ev = JSON.parse(line);
    if (ev.type === 'result' || ev.type === 'error') terminal = ev;
    else onEvent?.(ev);
  };
  for (;;) {
    const { value, done } = await reader.read();
    if (value) {
      buf += decoder.decode(value, { stream: !done });
      const lines = buf.split('\n');
      buf = lines.pop();
      lines.forEach(handle);
    }
    if (done) break;
  }
  handle(buf);
  return terminal ?? { type: 'error', error: { message: 'the run ended without a result', fields: [] }, warnings: [] };
}
