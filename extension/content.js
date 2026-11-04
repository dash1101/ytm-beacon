// Runs on music.youtube.com. Works out what is playing and reports it to the
// background script, which forwards it to the YTM Beacon app on this computer.
(() => {
  'use strict';

  const ext = globalThis.browser ?? globalThis.chrome;
  const alive = () => {
    try {
      return !!(ext.runtime && ext.runtime.id);
    } catch (_) {
      return false;
    }
  };

  // This file can be loaded twice: by the manifest, and again by the background
  // script for tabs that were already open when the extension was added. Only
  // one copy may run. A copy left behind by an extension reload is dead (its
  // runtime is gone), so it gets replaced.
  const prev = window.__ytmBeacon;
  if (prev && prev.alive()) return;
  if (prev) prev.stop();

  const SOURCE =
    typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : String(Math.random()).slice(2);
  const TICK_MS = 1000;
  const HEARTBEAT_MS = 5000;
  const SEEK_TOLERANCE_S = 2.5;

  let last = null; // last snapshot we reported
  let lastSentAt = 0;
  let sticky = { key: '', videoId: '' };
  let timer = null;

  window.__ytmBeacon = { alive, stop: () => clearInterval(timer) };

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

  // The main player's <video>. Signed-in pages can contain other <video>
  // elements (hover previews), so don't just take the first one.
  function mainVideo() {
    return (
      document.querySelector('#movie_player video') ||
      document.querySelector('video.html5-main-video') ||
      document.querySelector('video')
    );
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

  // What page.js (running inside the page) saw in navigator.mediaSession, or
  // null if it isn't running or nothing is set.
  function pageMetadata() {
    try {
      const raw = document.documentElement.getAttribute('data-ytm-beacon-meta');
      const m = raw ? JSON.parse(raw) : null;
      return m && typeof m === 'object' && m.title ? m : null;
    } catch (_) {
      return null;
    }
  }

  function nativeMetadata() {
    try {
      const md = navigator.mediaSession && navigator.mediaSession.metadata;
      return md && md.title ? md : null;
    } catch (_) {
      return null;
    }
  }

  // "Artist • Album • 2023" -> artist and album. Videos have "Channel • 1.8B views
  // • 19M likes", which has no album.
  function splitByline(text) {
    const parts = clean(text).split(' • ').filter(Boolean);
    const isStat = (s) => /^\d{4}$/.test(s) || /\b(views|likes|plays|subscribers)\b/i.test(s);
    return { artist: parts[0] || '', album: parts[1] && !isStat(parts[1]) ? parts[1] : '' };
  }

  // Last resort, when the page's media session has nothing: read the player on
  // screen. YouTube Music has used different layouts, so try each in turn.
  function domMetadata() {
    const text = (el) => clean(el && el.textContent);
    // Current layout: mini player at the bottom.
    const info = document.querySelector('ytmusic-track-info');
    if (info) {
      const title = text(info.querySelector('.ytmusicTrackInfoTitle'));
      if (title) {
        const items = Array.from(info.querySelectorAll('.ytmusicTrackInfoBylineItem')).map(text).filter(Boolean);
        const byline = items.length ? items.join(' • ') : text(info.querySelector('.ytmusicTrackInfoByline'));
        return { title, ...splitByline(byline) };
      }
    }
    // Earlier layout: the player bar.
    for (const bar of document.querySelectorAll('ytmusic-player-bar')) {
      const title = text(bar.querySelector('.title'));
      if (title) return { title, ...splitByline(text(bar.querySelector('.byline'))) };
    }
    // The queue marks the playing row.
    const row = document.querySelector('ytmusic-player-queue-item[selected]');
    const rowTitle = text(row && row.querySelector('.song-title'));
    if (rowTitle) return { title: rowTitle, ...splitByline(text(row.querySelector('.byline'))) };
    // The video's own title overlay, then the tab title.
    const overlay = text(document.querySelector('#movie_player .ytp-title-link'));
    if (overlay) return { title: overlay, artist: '', album: '' };
    const tabTitle = clean(document.title.replace(/\s*\|\s*YouTube Music$/, ''));
    if (tabTitle && tabTitle !== 'YouTube Music') return { title: tabTitle, artist: '', album: '' };
    return null;
  }

  function readMetadata() {
    // Order matters: the in-page copy is what the page really set, even when
    // another extension has replaced navigator.mediaSession.
    const md = pageMetadata() || nativeMetadata();
    let title = clean(md && md.title);
    let artist = clean(md && md.artist);
    let album = clean(md && md.album);
    const artwork = md ? largestArtwork(md.artwork) : '';

    if (!title) {
      const dom = domMetadata();
      if (dom) {
        title = dom.title;
        artist = artist || dom.artist;
        album = album || dom.album;
      }
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

  // Why there is (or isn't) something to report right now. The toolbar popup
  // asks this to explain problems in plain words.
  function inspect() {
    const video = mainVideo();
    if (!video) return { reason: 'no-player' };
    if (document.querySelector('.ad-showing')) return { reason: 'ad' };
    const meta = readMetadata();
    if (!meta.title) return { reason: 'nothing-loaded' };
    return { reason: 'ok', video, meta };
  }

  function snapshot() {
    const seen = inspect();
    if (seen.reason !== 'ok') return null;
    const { video, meta } = seen;
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
      if (!alive()) throw new Error('extension context gone');
      const p = ext.runtime.sendMessage(msg);
      if (p && typeof p.catch === 'function') p.catch(() => {});
    } catch (_) {
      // The extension was reloaded or updated; the background script will
      // start a fresh copy of this file in the tab.
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

  // The popup asks "are you running here, and what do you see?"
  ext.runtime.onMessage.addListener((msg, _sender, reply) => {
    if (!msg || msg.type !== 'probe') return;
    const seen = inspect();
    // What each source of song info says, to make problems easy to report.
    const out = {
      loaded: true,
      reason: seen.reason,
      pageScript: document.documentElement.hasAttribute('data-ytm-beacon-meta'),
      pageMetadata: !!pageMetadata(),
      nativeMetadata: !!nativeMetadata(),
    };
    if (seen.reason === 'ok') {
      out.title = seen.meta.title;
      out.artist = seen.meta.artist;
      out.state = seen.video.paused ? 'paused' : 'playing';
    }
    reply(out);
  });

  window.addEventListener('pagehide', () => send({ type: 'clear', source: SOURCE }));
  timer = setInterval(tick, TICK_MS);
  tick();
})();
