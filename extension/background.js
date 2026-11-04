// Forwards what the content script sees to the YTM Beacon app on this computer.
// Stateless on purpose: browsers may stop this script at any time, and the app
// decides which tab to show, so nothing here needs to survive a restart.
const ext = globalThis.browser ?? globalThis.chrome;

// Keep in sync with internal/server (the app uses the first free port in this range).
const PORTS = [41713, 41714, 41715, 41716, 41717];
let preferred = PORTS[0];

async function post(path, body) {
  const order = [preferred, ...PORTS.filter((p) => p !== preferred)];
  for (const port of order) {
    try {
      // text/plain keeps this a "simple" request, so no CORS preflight is needed.
      const res = await fetch(`http://127.0.0.1:${port}${path}`, {
        method: 'POST',
        headers: { 'Content-Type': 'text/plain' },
        body: JSON.stringify(body),
      });
      if (res.ok) {
        preferred = port;
        return true;
      }
    } catch (_) {
      // not listening on this port, try the next one
    }
  }
  return false;
}

function setBadge(tabId, ok) {
  if (tabId == null || !ext.action) return;
  const done = () => {};
  try {
    ext.action.setBadgeText({ tabId, text: ok ? '' : '!' }).catch(done);
    if (!ok) ext.action.setBadgeBackgroundColor({ tabId, color: '#d32f2f' }).catch(done);
  } catch (_) {
    // badge is only a hint
  }
}

ext.runtime.onMessage.addListener((msg, sender) => {
  if (!msg || (msg.type !== 'update' && msg.type !== 'clear')) return;
  const tabId = sender && sender.tab ? sender.tab.id : null;
  post(msg.type === 'update' ? '/v1/update' : '/v1/clear', msg).then((ok) => setBadge(tabId, ok));
});
