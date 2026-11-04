// Stand-in for main.py's Api, so index.html runs in a browser (see serve.py).
//
// ?s=<scenario>, one of:
//   checking downloading blocked failed dev     the update step
//   setup waiting help reading                  the steps before the result
//   r8 untested not-supported busy wrong-mode no-camera no-answer   result screens
// Add &theme=dark or &theme=light to force one. Buttons move through the steps the way
// the app does: Continue waits two seconds, finds an R8 and reads it.
// window.__state.calls is every API call the page made.
(() => {
  const app = window.__app;
  const q = new URLSearchParams(location.search);
  const scenario = q.get('s') || 'checking';
  if (q.get('theme')) document.documentElement.dataset.theme = q.get('theme');

  const state = window.__state = {
    step: 'update', version: app.version,
    update: { state: 'checking', version: null, reason: null }, update_note: '',
    waiting: { help: false, found: null }, reading: { camera: '' }, result: null, can_save_log: false,
    calls: [],
  };
  const result = name => Object.assign(state, { step: 'result', result: app.results[name], can_save_log: true });
  const timers = [];
  const later = (ms, f) => timers.push(setTimeout(f, ms));

  const scenarios = {
    checking: () => {},
    downloading: () => { state.update = { state: 'downloading', version: '0.5.1', reason: null }; },
    blocked: () => { state.update = { state: 'blocked', version: '0.5.1', reason: "Open Shutter Count isn't in your Applications folder" }; },
    failed: () => Object.assign(state, {
      step: 'setup', update: { state: 'failed', version: null, reason: "Couldn't reach GitHub (Could not resolve host: api.github.com)." },
      update_note: `Checking for updates didn't work (Couldn't reach GitHub (Could not resolve host: api.github.com)). You're reading with version ${app.version}.`,
    }),
    dev: () => Object.assign(state, { step: 'setup', update: { state: 'skipped', version: null, reason: 'running from source' } }),
    setup: () => { state.step = 'setup'; state.update = { state: 'current' }; },
    waiting: () => { state.step = 'waiting'; },
    help: () => { state.step = 'waiting'; state.waiting.help = true; },
    reading: () => { state.step = 'reading'; state.reading.camera = 'EOS R8'; },
  };
  (scenarios[scenario] || (() => result(scenario)))();

  function wait() {
    timers.forEach(clearTimeout);
    Object.assign(state, { step: 'waiting', waiting: { help: false, found: null }, result: null, can_save_log: false });
    later(1200, () => { state.waiting.help = true; });
    later(2000, () => Object.assign(state, { step: 'reading', reading: { camera: 'EOS R8' } }));
    later(3500, () => result('r8'));
  }

  const api = {
    state: () => JSON.parse(JSON.stringify(state)),
    setup_done: () => { if (state.step === 'setup') wait(); },
    back_to_setup: () => { timers.forEach(clearTimeout); state.step = 'setup'; },
    stop_looking: () => { timers.forEach(clearTimeout); result('no-camera'); },
    read_again: () => wait(),
    save_log: () => ({ path: '/Users/you/Desktop/Open Shutter Count log.txt', name: 'Open Shutter Count log.txt' }),
    open_issue: () => null,
    download_update: () => null,
    quit: () => null,
    log_error: message => console.error('page error:', message),
  };
  window.pywebview = { api: {} };
  for (const [name, f] of Object.entries(api)) {
    window.pywebview.api[name] = (...args) => { state.calls.push(name); return Promise.resolve(f(...args)); };
  }
})();
