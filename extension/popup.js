// Toolbar popup: a quick "is everything connected?" check with plain-English fixes.
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

function row(kind, text, detail) {
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
  if (seen == null) {
    checks.append(row('warn', 'No music detected yet', 'Open music.youtube.com and play a song.'));
  } else if (seen > 20) {
    checks.append(row('warn', 'Music tab went quiet', 'Reload your YouTube Music tab if this keeps happening.'));
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
