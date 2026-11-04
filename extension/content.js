// Runs on music.youtube.com. Works out what is playing and reports it to the
// background script, which forwards it to the YTM Beacon app on this computer.
(() => {
  'use strict';
  if (window.__ytmBeaconLoaded) return;
  window.__ytmBeaconLoaded = true;

  const ext = globalThis.browser ?? globalThis.chrome;
  const SOURCE =
    typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : String(Math.random()).slice(2);
  const TICK_MS = 1000;
  const HEARTBEAT_MS = 5000;
  const SEEK_TOLERANCE_S = 2.5;

  let last = null; // last snapshot we reported
  let lastSentAt = 0;
  let sticky = { key: '', videoId: '' };
  let timer = null;

  const clean = (s) => (s || '').replace(/\s+/g, ' ').trim();

  function browserName() {
    if (navigator.brave) return 'Brave';
    const ua = navigator.userAgent;
    if (/Firefox\//.test(ua)) return 'Firefox';
    if (/Edg\//.test(ua)) return 'Edge';
    if (/OPR\//.test(ua)) return 'Opera';
    if (/Chrome\//.test(ua)) return 'Chrome';
    return 'Browser';
  }

  function largestArtwork(list) {
    let best = '';
    let bestArea = -1;
    for (const a of list || []) {
      if (!a || !/^https:\/\//.test(a.src || '')) continue;
      const m = /(\d+)x(\d+)/.exec(a.sizes || '');
      const area = m ? Number(m[1]) * Number(m[2]) : 0;
      if (area > bestArea) {
        best = a.src;
        bestArea = area;
      }
    }
    return best;
  }

  // Google image URLs end in a size suffix (=w60-h60-l90-rj); ask for a bigger one.
  function upscaleArtwork(url) {
    if (/^https:\/\/(lh\d|yt\d)\.(googleusercontent|ggpht)\.com\//.test(url)) {
      return url.replace(/=(?:w\d+-h\d+|s\d+)[^/?]*$/, '=w544-h544-l90-rj');
    }
    return url;
  }

  function readMetadata() {
    const md = navigator.mediaSession && navigator.mediaSession.metadata;
    let title = clean(md && md.title);
    let artist = clean(md && md.artist);
    let album = clean(md && md.album);
    const artwork = md ? largestArtwork(md.artwork) : '';

    if (!title) {
      // Fallback: read the player bar at the bottom of the page.
      const bar = document.querySelector('ytmusic-player-bar');
      const titleEl = bar && bar.querySelector('.title');
      const bylineEl = bar && bar.querySelector('.byline');
      title = clean(titleEl && titleEl.textContent);
      const parts = clean(bylineEl && bylineEl.textContent).split(' • ');
      artist = artist || parts[0] || '';
      album = album || parts[1] || '';
    }
    artist = artist.replace(/ - Topic$/, '');
    return { title, artist, album, artwork: upscaleArtwork(artwork) };
  }

  function currentVideoId(meta) {
    let id = '';
    if (location.pathname === '/watch') {
      id = new URLSearchParams(location.search).get('v') || '';
    }
    if (!id && meta.artwork) {
      const m = /\/vi(?:_webp)?\/([\w-]{11})\//.exec(meta.artwork);
      if (m) id = m[1];
    }
    if (!id) {
      const a = document.querySelector('ytmusic-player-bar a[href*="watch?v="]');
      const m = a && /[?&]v=([\w-]{11})/.exec(a.getAttribute('href') || '');
      if (m) id = m[1];
    }
    // While browsing other pages the URL says nothing about the playing track,
    // so remember the last id we saw for this exact title/artist.
    const key = meta.title + '\u0001' + meta.artist;
    if (id) sticky = { key, videoId: id };
    else if (sticky.key === key) id = sticky.videoId;
    return id;
  }

  function snapshot() {
    const video = document.querySelector('video');
    if (!video) return null;
    if (document.querySelector('.ad-showing')) return null;
    const meta = readMetadata();
    if (!meta.title) return null;
    const duration = Number.isFinite(video.duration) ? video.duration : 0;
    return {
      ...meta,
      videoId: currentVideoId(meta),
      state: video.paused || video.ended ? 'paused' : 'playing',
      position: Math.max(0, Math.round(video.currentTime * 10) / 10),
      duration: Math.round(duration * 10) / 10,
    };
  }

  function send(msg) {
    try {
      if (!ext.runtime || !ext.runtime.id) throw new Error('extension context gone');
      const p = ext.runtime.sendMessage(msg);
      if (p && typeof p.catch === 'function') p.catch(() => {});
    } catch (_) {
      // The extension was reloaded or updated; this page needs a refresh to reconnect.
      clearInterval(timer);
    }
  }

  function tick() {
    const s = snapshot();
    const now = Date.now();
    if (!s) {
      if (last) {
        send({ type: 'clear', source: SOURCE });
        last = null;
      }
      return;
    }

    const key = [s.videoId, s.title, s.artist, s.state].join('\u0001');
    let changed = !last || last.key !== key;
    if (!changed && Math.abs(s.duration - last.duration) > 1) changed = true;
    if (!changed && s.state === 'playing') {
      const expected = last.position + (now - lastSentAt) / 1000;
      if (Math.abs(s.position - expected) > SEEK_TOLERANCE_S) changed = true; // user seeked
    }

    if (changed || now - lastSentAt >= HEARTBEAT_MS) {
      send({ type: 'update', source: SOURCE, agent: browserName(), ...s });
      last = { ...s, key };
      lastSentAt = now;
    }
  }

  window.addEventListener('pagehide', () => send({ type: 'clear', source: SOURCE }));
  timer = setInterval(tick, TICK_MS);
  tick();
})();
