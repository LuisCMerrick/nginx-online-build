const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function client() {
  const elements = new Map();
  const element = id => {
    if (!elements.has(id)) {
      const classes = new Set();
      elements.set(id, { disabled: true, textContent: '', innerHTML: '',
        classList: { add: x => classes.add(x), remove: x => classes.delete(x),
          contains: x => classes.has(x), toggle: (x, on) => on ? classes.add(x) : classes.delete(x) } });
    }
    return elements.get(id);
  };
  const storage = new Map([['nginx_builder_key', 'legacy-secret']]);
  const state = { closed: 0, cleared: 0, calls: [] };
  const context = vm.createContext({ console,
    document: { addEventListener() {}, querySelector: () => null, querySelectorAll: () => [], getElementById: element },
    location: { pathname: '/nginx/', search: '?key=legacy-secret' },
    localStorage: { getItem: k => storage.get(k), removeItem: k => storage.delete(k) },
    clearInterval: () => state.cleared++,
    fetch: async (url, opts) => {
      state.calls.push({ url, opts });
      if (url.endsWith('/cancel')) return { json: async () => ({ success: true }) };
      if (url.endsWith('/api/builds')) return { json: async () => ({ success: true, builds: [] }) };
      return { status: 200, json: async () => ({ success: true, build: { status: 'cancelled', target_os: 'linux', target_arch: 'amd64' } }) };
    },
    alert: msg => assert.fail(msg)
  });
  context.window = context;
  vm.runInContext(fs.readFileSync(`${__dirname}/i18n.js`, 'utf8'), context);
  vm.runInContext(fs.readFileSync(`${__dirname}/app.js`, 'utf8'), context);
  context.closeSource = () => state.closed++;
  vm.runInContext('activeBuildId = "build-1"; pollTimer = 1; logEventSource = {close: closeSource};', context);
  return { context, element, state, storage };
}

test('API and SSE URLs contain no persisted access key', () => {
  const { context, storage } = client();
  assert.equal(storage.has('nginx_builder_key'), false);
  assert.equal(context.apiUrl('/api/builds/build-1/logs?stream=true'), '/nginx/api/builds/build-1/logs?stream=true');
  assert.equal(context.apiUrl('/nginx/api/builds'), '/nginx/api/builds');
});

test('cancelled is terminal and cancel action restores the controls', async () => {
  const { context, element, state } = client();
  await context.cancelActiveBuild();
  assert.equal(state.calls[0].url, '/nginx/api/builds/build-1/cancel');
  assert.equal(state.calls[0].opts.method, 'POST');
  assert.ok(state.closed > 0);
  assert.ok(state.cleared > 0);
  assert.equal(element('start-build-btn').disabled, false);
  assert.equal(element('cancel-build-btn').disabled, false);
  assert.equal(element('cancel-build-btn').classList.contains('hidden'), true);
  assert.equal(element('task-status-pill').textContent, 'Cancelled');
});
