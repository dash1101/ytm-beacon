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

// Hide, show or toggle the presence. The popup talks to the app directly.
async function setVisibility(port, action, minutes) {
  await fetch(`http://127.0.0.1:${port}/v1/visibility`, {
    method: 'POST',
    headers: { 'Content-Type': 'text/plain' },
    body: JSON.stringify({ action, minutes }),
  });
}

// "Show when paused": keep the song on your profile (with a pause icon) while
// paused, or hide the card completely the moment you pause.
function renderPausedToggle(port, status) {
  const row = document.getElementById('pausedRow');
  const box = document.getElementById('showPaused');
  row.hidden = false;
  box.checked = !!status.config.showPaused;
  box.onchange = async () => {
    box.disabled = true;
    try {
      await fetch(`http://127.0.0.1:${port}/v1/config`, {
        method: 'POST',
        headers: { 'Content-Type': 'text/plain' },
        body: JSON.stringify({ showPaused: box.checked }),
      });
    } catch (_) {
      // the next render shows the real state
    }
    box.disabled = false;
    render();
  };
}

function minutesLeft(sec) {
  const m = Math.max(1, Math.ceil(sec / 60));
  return m >= 120 ? `${Math.round(m / 60)} hours` : m >= 60 ? 'an hour' : `${m} minute${m === 1 ? '' : 's'}`;
}

// The keyboard shortcut as the browser has it set (users can change or clear it).
async function shortcutText() {
  try {
    const all = await ext.commands.getAll();
    const c = all.find((x) => x.name === 'toggle-presence');
    return (c && c.shortcut) || '';
  } catch (_) {
    return '';
  }
}

// The on / off control at the top: the quickest way to stop showing on Discord.
async function renderPower(port, status) {
  const shortcut = await shortcutText();
  const box = document.getElementById('power');
  box.textContent = '';
  box.hidden = false;
  const what = document.createElement('div');
  what.className = 'what';
  const sub = document.createElement('small');
  const button = (label, action, minutes, primary) => {
    const b = document.createElement('button');
    b.textContent = label;
    if (primary) b.className = 'primary';
    b.addEventListener('click', async () => {
      b.disabled = true;
      try {
        await setVisibility(port, action, minutes);
      } catch (_) {
        // the next render shows the real state
      }
      render();
    });
    return b;
  };
  const buttons = [];
  if (status.visible) {
    box.className = '';
    what.append('Showing on Discord', sub);
    sub.textContent = shortcut ? `${shortcut} switches it off` : 'Stops until you turn it back on';
    buttons.push(button('Stop', 'hide', 0), button('1 hour', 'hide', 60));
  } else if (!status.config.enabled) {
    box.className = 'off';
    what.append('Switched off', sub);
    sub.textContent = 'Nothing is shown on Discord';
    buttons.push(button('Show again', 'show', 0, true));
  } else {
    box.className = 'off';
    what.append('Hidden for now', sub);
    sub.textContent = `Back in ${minutesLeft(status.snoozeSec)}`;
    buttons.push(button('Show now', 'show', 0, true));
  }
  box.append(what, ...buttons);
}

// Release builds are stamped with the tag ("v0.1.2"); a dev build says "dev".
function versionLabel(v) {
  v = String(v || '');
  return /^v?\d/i.test(v) ? 'v' + v.replace(/^v/i, '') : v;
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
      return row('warn', 'Nothing is playing yet', 'Play a song in the YouTube Music tab. ' +
        `(page script ${p.pageScript ? 'on' : 'off'}, in-page song ${p.pageMetadata ? 'found' : 'none'}, browser song ${p.nativeMetadata ? 'found' : 'none'})`);
    default:
      return row('warn', "YouTube Music's player isn't ready", 'Wait a moment, or reload the tab.');
  }
}

async function render() {
  const checks = document.getElementById('checks');
  const hint = document.getElementById('hint');
  checks.textContent = '';
  hint.hidden = true;
  document.getElementById('power').hidden = true;
  document.getElementById('pausedRow').hidden = true;

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
  document.getElementById('version').textContent = versionLabel(status.version);
  document.getElementById('dashboard').href = `http://127.0.0.1:${port}/`;
  await renderPower(port, status);
  renderPausedToggle(port, status);

  checks.append(row('ok', 'YTM Beacon app is running'));
  checks.append(discordRow(status.discord));

  const seen = status.extension && status.extension.lastSeenAgoSec;
  // A paused tab in the background only reports about once a minute, so "quiet" is the app's number.
  const quiet = (status.extension && status.extension.quietAfterSec) || 20;
  if (seen == null || seen > quiet) {
    checks.append(await musicRow(seen));
  } else {
    checks.append(row('ok', 'Receiving from YouTube Music'));
  }

  const np = status.nowPlaying;
  if (status.presenceActive && np) {
    // Private mode shows nothing about the song, so the popup doesn't either.
    const detail = status.config.privateMode ? 'Private mode: only "Listening to music"' : `${np.title} — ${np.artist}`;
    checks.append(row('ok', 'Showing on Discord', detail));
  } else if (np && status.hidden === 'filtered') {
    checks.append(row('warn', 'Not shown: matches your "never show" words', 'Change them on the dashboard.'));
  } else if (np && (status.hidden === 'off' || status.hidden === 'snoozed')) {
    checks.append(row('warn', 'Not showing on Discord', 'You turned it off (see the switch above).'));
  } else if (np && np.state === 'paused') {
    checks.append(row('warn', 'Paused', 'Presence is hidden while paused (see dashboard settings).'));
  } else {
    checks.append(row('warn', 'Not showing on Discord', 'Play a song to start.'));
  }
}

render();
