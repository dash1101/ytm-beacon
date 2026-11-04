// Runs inside the YouTube Music page itself (the "MAIN" world), not in the
// extension's private world.
//
// Why: some other extensions (ad blockers, YouTube enhancers) replace
// navigator.mediaSession inside the page. YouTube Music then writes the song
// into the replacement, and the extension's private copy of the browser, which
// still sees the real, empty object, finds no song. Reading it from here shows
// exactly what the page set, whoever intercepted it.
//
// It only copies the current song info into an attribute on <html> for
// content.js to read. It sends nothing anywhere.
(() => {
  'use strict';
  if (window.__ytmBeaconPage) return;
  window.__ytmBeaconPage = true;

  const ATTR = 'data-ytm-beacon-meta';
  let lastJSON = '';

  function read() {
    let md = null;
    try {
      md = navigator.mediaSession && navigator.mediaSession.metadata;
    } catch (_) {
      return null;
    }
    if (!md) return null;
    const artwork = [];
    try {
      for (const a of md.artwork || []) {
        if (a && typeof a.src === 'string' && /^https:\/\//.test(a.src)) {
          artwork.push({ src: a.src, sizes: String(a.sizes || '') });
        }
      }
    } catch (_) {
      // artwork is optional
    }
    return {
      title: String(md.title || ''),
      artist: String(md.artist || ''),
      album: String(md.album || ''),
      artwork: artwork.slice(0, 8),
    };
  }

  function publish() {
    const json = JSON.stringify(read());
    if (json === lastJSON) return;
    lastJSON = json;
    try {
      document.documentElement.setAttribute(ATTR, json);
    } catch (_) {
      // nothing else to do
    }
  }

  setInterval(publish, 500);
  publish();
})();
