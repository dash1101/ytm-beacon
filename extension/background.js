// Forwards what the content script sees to the YTM Beacon app on this computer.
// Stateless on purpose: browsers may stop this script at any time, and the app
// decides which tab to show, so nothing here needs to survive a restart.
const ext = globalThis.browser ?? globalThis.chrome;

// Keep in sync with internal/server (the app uses the first free port in this range).
const PORTS = [41713, 41714, 41715, 41716, 41717];
let preferred = PORTS[0];

// Identifies this copy of the extension's code. Filled in when the app unpacks
// the extension; it stays a placeholder when run straight from the source tree.
const BUILD = '__BUILD__';
const PACKAGED = !BUILD.startsWith('__');

// Updating the app replaces the extension's files on disk, but a browser keeps
// running the old code until the extension is reloaded. The app tells us which
// build it ships, so when ours is older we reload ourselves: no clicking on the
// browser's extensions page. Once per build, so a copy that can't update (for
// example one installed from a store) never gets stuck in a reload loop.
async function reloadIfOutdated(appBuild) {
  if (!PACKAGED || !appBuild || appBuild === BUILD) return;
  try {
    const { reloadedFor } = await ext.storage.local.get('reloadedFor');
    if (reloadedFor === appBuild) return;
    await ext.storage.local.set({ reloadedFor: appBuild });
    ext.runtime.reload();
  } catch (_) {
    // without storage we cannot guard against loops, so do nothing
  }
}

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
        try {
          reloadIfOutdated((await res.json()).extensionBuild);
        } catch (_) {
          // the reply is only a hint
        }
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

// Browsers only run content scripts in pages loaded after the extension was
// added, so a YouTube Music tab that was already open would stay silent until
// reloaded. Start the scripts in those tabs ourselves. They ignore themselves
// if the manifest already started them.
async function injectIntoOpenTabs() {
  if (!ext.scripting || !ext.tabs) return;
  let tabs = [];
  try {
    tabs = await ext.tabs.query({ url: 'https://music.youtube.com/*' });
  } catch (_) {
    return;
  }
  for (const tab of tabs) {
    try {
      await ext.scripting.executeScript({ target: { tabId: tab.id }, files: ['page.js'], world: 'MAIN' });
      await ext.scripting.executeScript({ target: { tabId: tab.id }, files: ['content.js'] });
    } catch (_) {
      // discarded or not-yet-loaded tab: it gets the script when it loads
    }
  }
}

ext.runtime.onInstalled.addListener(injectIntoOpenTabs);
if (ext.runtime.onStartup) ext.runtime.onStartup.addListener(injectIntoOpenTabs);
