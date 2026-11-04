// Toolbar popup: a quick "is everything connected?" check with plain-English fixes.
const ext = globalThis.browser ?? globalThis.chrome;
const PORTS = [41713, 41714, 41715, 41716, 41717]; // keep in sync with background.js

async function fetchStatus() {
  for (const port of PORTS) {
    try {
      const ctl = new AbortController();
      const t = setTimeout(() => ctl.abort(), 1200);
      const res = await fetch(`http://127.0.0.1:${port}/v1/status`, { signal: ctl.signal });
      clearTimeout(t);
      if (res.ok) {
        const s = await res.json();
        if (s && s.app === 'ytm-beacon') return { port, status: s };
      }
    } catch (_) {
      // try the next port
    }
  }
  return null;
}

function row(kind, text, detail, action) {
  const li = document.createElement('li');
  li.className = kind;
  const mark = document.createElement('span');
  mark.className = 'mark';
  mark.textContent = kind === 'ok' ? '✓' : kind === 'warn' ? '!' : '✗';
  const body = document.createElement('span');
  body.textContent = text;
  if (detail) {
    const d = document.createElement('span');
    d.className = 'detail';
    d.textContent = detail;
    body.appendChild(d);
  }
  if (action) {
    const b = document.createElement('button');
    b.textContent = action.label;
    b.addEventListener('click', async () => {
      b.disabled = true;
      try {
        await action.run();
      } catch (_) {
        // the next render shows the real state
      }
      setTimeout(render, 2500);
    });
    body.appendChild(b);
  }
  li.append(mark, body);
  return li;
}

function discordRow(d) {
  switch (d.state) {
    case 'connected':
      return row('ok', 'Connected to Discord', d.user ? `Signed in as ${d.user}` : '');
    case 'no-client-id':
      return row('bad', 'No Discord application ID set', 'Open the dashboard to add one.');
    default:
      return row('bad', "Can't reach Discord", d.lastError || 'Is the Discord app running?');
  }
}

// Ask every open YouTube Music tab whether our script is running in it and
// what it sees. Returns null if the browser doesn't let us look.
async function inspectTabs() {
  if (!ext.tabs || !ext.tabs.query) return null;
  let tabs;
  try {
    tabs = await ext.tabs.query({ url: 'https://music.youtube.com/*' });
  } catch (_) {
    return null;
  }
  const out = [];
  for (const tab of tabs) {
    let probe = null;
    try {
      probe = await Promise.race([
        ext.tabs.sendMessage(tab.id, { type: 'probe' }),
        new Promise((_, rej) => setTimeout(() => rej(new Error('timeout')), 1000)),
      ]);
    } catch (_) {
      probe = null; // nothing is listening in that tab
    }
    out.push({ tab, probe });
  }
  return out;
}

// Explains why nothing is being reported, in terms of what the tab itself says.
async function musicRow(heardRecently) {
  const tabs = await inspectTabs();
  if (tabs === null) {
    return heardRecently === null
      ? row('warn', 'No music detected yet', 'Open music.youtube.com and play a song.')
      : row('warn', 'Music tab went quiet', 'Reload your YouTube Music tab.');
  }
  if (tabs.length === 0) {
    return row('warn', 'No YouTube Music tab is open', 'Open music.youtube.com and play a song.');
  }
  const dead = tabs.find((t) => !t.probe);
  if (dead) {
    return row('bad', "The extension isn't running in your YouTube Music tab", 'Tabs opened before the extension was added need a reload.', {
      label: 'Reload the tab',
      run: () => ext.tabs.reload(dead.tab.id),
    });
  }
  const p = (tabs.find((t) => t.probe.reason === 'ok') || tabs[0]).probe;
  switch (p.reason) {
    case 'ok':
      return row('bad', `Sees "${p.title}" but the app isn't receiving it`, 'Run ytm-beacon doctor, or open the dashboard.');
    case 'ad':
      return row('warn', 'An ad is playing', 'Nothing is shown on Discord during ads.');
    case 'nothing-loaded':
      return row('warn', 'Nothing is playing yet', 'Play a song in the YouTube Music tab.');
    default:
      return row('warn', "YouTube Music's player isn't ready", 'Wait a moment, or reload the tab.');
  }
}

async function render() {
  const checks = document.getElementById('checks');
  const hint = document.getElementById('hint');
  checks.textContent = '';
  hint.hidden = true;

  const found = await fetchStatus();
  if (!found) {
    checks.append(row('bad', 'YTM Beacon app is not running'));
    hint.textContent =
      'Install or start the YTM Beacon app on this computer, then reopen this popup. ' +
      'See the Help link for download and setup steps.';
    hint.hidden = false;
    return;
  }

  const { port, status } = found;
  document.getElementById('version').textContent = `v${status.version}`;
  document.getElementById('dashboard').href = `http://127.0.0.1:${port}/`;

  checks.append(row('ok', 'YTM Beacon app is running'));
  checks.append(discordRow(status.discord));

  const seen = status.extension && status.extension.lastSeenAgoSec;
  if (seen == null || seen > 20) {
    checks.append(await musicRow(seen));
  } else {
    checks.append(row('ok', 'Receiving from YouTube Music'));
  }

  const np = status.nowPlaying;
  if (status.presenceActive && np) {
    checks.append(row('ok', 'Showing on Discord', `${np.title} — ${np.artist}`));
  } else if (np && np.state === 'paused') {
    checks.append(row('warn', 'Paused', 'Presence is hidden while paused (see dashboard settings).'));
  } else {
    checks.append(row('warn', 'Not showing on Discord', 'Play a song to start.'));
  }
}

render();
