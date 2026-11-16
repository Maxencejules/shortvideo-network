/**
 * Viewer styles. Motion follows the client setting and the OS media query.
 */

export const VIEWER_CSS = `
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
body { margin: 0; font: 16px/1.45 system-ui, sans-serif; }
button, input { font: inherit; }
.banner { padding: 0.6rem 1rem; background: #1c1c1c; color: #f5f5f5; }
.nav { display: flex; flex-wrap: wrap; gap: 0.4rem; padding: 0.5rem 1rem; }
.nav button[aria-current="page"] { outline: 2px solid currentColor; }
.layout { display: grid; gap: 1rem; padding: 0 1rem 2rem; }
.stage { width: 100%; max-width: 420px; margin: 0 auto; }
.feed { max-height: calc(100dvh - 9rem); overflow-y: auto; scroll-snap-type: y mandatory; }
.feed.smooth { scroll-behavior: smooth; }
.slide { min-height: 70dvh; scroll-snap-align: start; display: grid; align-content: center; gap: 0.6rem; }
video { display: block; width: 100%; max-width: 420px; max-height: 78dvh; background: #111; aspect-ratio: 9 / 16; object-fit: contain; }
pre { white-space: pre-wrap; overflow-wrap: anywhere; max-width: 100%; }
.controls, .gates, .list { display: flex; flex-wrap: wrap; gap: 0.4rem; }
.gate, .note, .panel { border: 1px solid currentColor; padding: 0.6rem 0.75rem; }
.caption { min-height: 1.4em; background: #111; color: #fff; padding: 0.2rem 0.4rem; }
:focus-visible { outline: 3px solid #0b57d0; outline-offset: 2px; }
@media (max-width: 720px) {
  .layout { padding: 0 0 2rem; }
  .stage, video { width: 100%; max-width: 100%; }
  .slide { min-height: calc(100dvh - 8rem); }
}
@media (prefers-reduced-motion: reduce) {
  .feed { scroll-behavior: auto; }
}
.is-reduced .feed { scroll-behavior: auto; }
`.trim();
