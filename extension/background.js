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

// Returns the app's reply (an object), or null when no app answered.
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
        let reply = {};
        try {
          reply = (await res.json()) || {};
        } catch (_) {
          // the reply is only a hint
        }
        reloadIfOutdated(reply.extensionBuild);
        return reply;
      }
    } catch (_) {
      // not listening on this port, try the next one
    }
  }
  return null;
}

// The toolbar icon says what is wrong ("!") or that the presence is switched
// off or hidden for a while ("off"). Nothing when all is well.
function badgeFor(reply) {
  if (!reply) return { text: '!', color: '#d32f2f' };
  if (reply.visible === false) return { text: 'off', color: '#6b7280' };
  return { text: '', color: '#6b7280' };
}

function setBadge(tabId, reply) {
  if (!ext.action) return;
  const { text, color } = badgeFor(reply);
  const done = () => {};
  try {
    // Without a tab the badge applies to every tab. Only an answer from the app
    // says anything about the presence, so a failure marks just the one tab.
    if (reply) ext.action.setBadgeText({ text }).catch(done);
    if (tabId != null) ext.action.setBadgeText({ tabId, text }).catch(done);
    if (text) ext.action.setBadgeBackgroundColor({ color }).catch(done);
  } catch (_) {
    // badge is only a hint
  }
}

ext.runtime.onMessage.addListener((msg, sender) => {
  if (!msg || (msg.type !== 'update' && msg.type !== 'clear')) return;
  const tabId = sender && sender.tab ? sender.tab.id : null;
  post(msg.type === 'update' ? '/v1/update' : '/v1/clear', msg).then((reply) => setBadge(tabId, reply));
});

// Stop or resume showing on Discord, and show the result on the toolbar icon.
async function toggleVisibility() {
  const reply = await post('/v1/visibility', { action: 'toggle' });
  setBadge(null, reply);
  // Open music tabs would otherwise keep their old badge until their next update.
  try {
    for (const tab of await ext.tabs.query({ url: 'https://music.youtube.com/*' })) setBadge(tab.id, reply);
  } catch (_) {
    // the next update fixes it
  }
}

// Keyboard shortcut: Alt+Shift+M unless the user changed it on the browser's
// extension shortcuts page.
if (ext.commands && ext.commands.onCommand) {
  ext.commands.onCommand.addListener((name) => {
    if (name === 'toggle-presence') toggleVisibility();
  });
}

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
