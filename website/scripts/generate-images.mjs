/**
 * Generates static image assets from SVG sources:
 * - favicon-32.png     (32x32, for <link rel="icon" sizes="32x32">)
 * - apple-touch-icon.png (180x180)
 * - og.png             (1200x630, for og:image and twitter:image)
 *
 * Run via `npm run images`. Reads public/favicon.svg and
 * public/illustrations/moltnet-hero.svg, freezes the hero's animated
 * pulses at trace midpoints for a lively static OG frame.
 */

import sharp from 'sharp';
import { readFileSync } from 'fs';
import { join } from 'path';

const PUBLIC = new URL('../public', import.meta.url).pathname;

const FAVICON = join(PUBLIC, 'favicon.svg');
const HERO = join(PUBLIC, 'illustrations/moltnet-hero.svg');

// --- favicon PNGs ---
await sharp(FAVICON).resize(32, 32).png().toFile(join(PUBLIC, 'favicon-32.png'));
await sharp(FAVICON).resize(180, 180).png().toFile(join(PUBLIC, 'apple-touch-icon.png'));

// --- og.png ---
// Trace midpoints, computed from path d-attributes in the hero SVG.
const PULSE_POSITIONS = {
  'openclaw-trace':   { x: 512.375, y: 224 },
  'picoclaw-trace':   { x: 442.5,   y: 378.5 },
  'codex-trace':      { x: 841,     y: 378.5 },
  'tinyclaw-trace':   { x: 493.25,  y: 532.5 },
  'claudecode-trace': { x: 774.75,  y: 532.5 },
};

const hero = readFileSync(HERO, 'utf8');

// Replace each animated pulse circle with a static one at its trace midpoint.
// Halos get r=11 / opacity=0.4; cores get r=5 / opacity=1.
const pulsePattern = /<circle\s+r="(\d+)"\s+fill="#6ef5a7"\s+fill-opacity="0">\s*<animateMotion[^>]*>\s*<mpath href="#([a-z]+-trace)"[^/]*\/>\s*<\/animateMotion>\s*<animate[^/]*\/>\s*<\/circle>/g;

let frozen = hero.replace(pulsePattern, (_m, r, traceId) => {
  const pos = PULSE_POSITIONS[traceId];
  if (!pos) return '';
  const opacity = r === '11' ? '0.4' : '1';
  return `<circle cx="${pos.x}" cy="${pos.y}" r="${r}" fill="#6ef5a7" fill-opacity="${opacity}"/>`;
});

// Strip any remaining <animate ...> tags (e.g. the hub dot blink).
frozen = frozen.replace(/<animate\s[^>]*\/>/g, '');

// Pull the inner content out of the hero's <svg> wrapper so we can nest it
// inside an OG-shaped canvas.
const innerMatch = frozen.match(/<svg[^>]*>([\s\S]*)<\/svg>/);
if (!innerMatch) throw new Error('hero SVG parse failed');
const innerContent = innerMatch[1];

// Hero is 1280x720 (16:9). OG is 1200x630 (~1.9:1). Scale by height (0.875),
// width becomes 1120, pad 40px each side on a dark canvas.
const ogSvg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1200 630">
  <rect width="1200" height="630" fill="#0b0f13"/>
  <g transform="translate(40 0) scale(0.875)">${innerContent}</g>
</svg>`;

await sharp(Buffer.from(ogSvg), { density: 300 })
  .resize(1200, 630)
  .png()
  .toFile(join(PUBLIC, 'og.png'));

console.log('Generated favicon-32.png, apple-touch-icon.png, og.png');
