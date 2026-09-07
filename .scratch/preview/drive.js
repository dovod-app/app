// Minimal CDP driver: load pages, collect console errors + failed requests, screenshot.
const CDP_PORT = process.env.CDP_PORT || 9333;

async function http(path) {
  const r = await fetch(`http://127.0.0.1:${CDP_PORT}${path}`, { method: 'PUT' });
  return r.json();
}

async function visit(url, label) {
  const target = await http('/json/new?' + encodeURIComponent(url));
  const ws = new WebSocket(target.webSocketDebuggerUrl);
  const errors = [];
  const failed = [];
  let id = 0;
  const send = (method, params = {}) => ws.send(JSON.stringify({ id: ++id, method, params }));

  await new Promise((res) => (ws.onopen = res));
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data);
    if (msg.method === 'Runtime.consoleAPICalled' && ['error', 'warning'].includes(msg.params.type)) {
      errors.push(msg.params.type + ': ' + msg.params.args.map((a) => a.value ?? a.description ?? a.type).join(' '));
    }
    if (msg.method === 'Runtime.exceptionThrown') {
      errors.push('exception: ' + (msg.params.exceptionDetails.exception?.description || msg.params.exceptionDetails.text));
    }
    if (msg.method === 'Network.responseReceived' && msg.params.response.status >= 400) {
      failed.push(msg.params.response.status + ' ' + msg.params.response.url);
    }
  };
  send('Runtime.enable');
  send('Network.enable');
  send('Page.enable');
  await new Promise((r) => setTimeout(r, 4000));

  // grab visible text
  const evalId = ++id;
  ws.send(JSON.stringify({ id: evalId, method: 'Runtime.evaluate', params: { expression: 'document.body.innerText.slice(0,700)', returnByValue: true } }));
  const text = await new Promise((res) => {
    const h = (m) => { const msg = JSON.parse(m.data); if (msg.id === evalId) { ws.removeEventListener('message', h); res(msg.result?.result?.value || ''); } };
    ws.addEventListener('message', h);
    setTimeout(() => res(''), 3000);
  });

  console.log('\n########## ' + label + '  ' + url);
  console.log('--- text ---\n' + text.replace(/\n{2,}/g, '\n'));
  if (errors.length) console.log('--- console ---\n' + [...new Set(errors)].slice(0, 12).join('\n'));
  if (failed.length) console.log('--- failed requests ---\n' + [...new Set(failed)].slice(0, 12).join('\n'));
  ws.close();
  await fetch(`http://127.0.0.1:${CDP_PORT}/json/close/${target.id}`);
}

(async () => {
  for (const [url, label] of JSON.parse(process.env.TARGETS)) {
    try { await visit(url, label); } catch (e) { console.log('FAILED ' + label + ': ' + e.message); }
  }
})();
