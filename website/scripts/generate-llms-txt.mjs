/**
 * Generates llms.txt, llms-full.txt, and copies raw .md files into dist/.
 * Run after `astro build`.
 *
 * llms.txt      — index linking to raw .md files
 * llms-full.txt — all docs concatenated as markdown
 * docs/*.md     — raw markdown files accessible at /docs/<slug>.md
 */

import { readFileSync, writeFileSync, readdirSync, statSync, mkdirSync, copyFileSync } from 'fs';
import { join, relative, dirname } from 'path';

const DOCS_DIR = new URL('../src/content/docs', import.meta.url).pathname;
const DIST_DIR = new URL('../dist', import.meta.url).pathname;
const SITE_URL = 'https://moltnet.dev';

function walkDir(dir) {
  const files = [];
  for (const entry of readdirSync(dir)) {
    const fullPath = join(dir, entry);
    if (statSync(fullPath).isDirectory()) {
      files.push(...walkDir(fullPath));
    } else if (entry.endsWith('.md') || entry.endsWith('.mdx')) {
      files.push(fullPath);
    }
  }
  return files;
}

function extractFrontmatter(content) {
  const match = content.match(/^---\n([\s\S]*?)\n---/);
  if (!match) return {};
  const fm = {};
  for (const line of match[1].split('\n')) {
    const [key, ...rest] = line.split(':');
    if (key && rest.length) {
      fm[key.trim()] = rest.join(':').trim();
    }
  }
  return fm;
}

function getSlug(filePath) {
  let slug = relative(DOCS_DIR, filePath)
    .replace(/\.mdx?$/, '')
    .replace(/\/index$/, '');
  if (slug === 'index') slug = '';
  return slug;
}

const files = walkDir(DOCS_DIR).sort();

const docsOutDir = join(DIST_DIR, 'docs');
for (const file of files) {
  const slug = getSlug(file);
  if (!slug) continue;
  const outPath = join(docsOutDir, `${slug}.md`);
  mkdirSync(dirname(outPath), { recursive: true });
  copyFileSync(file, outPath);
}

const indexLines = [
  '# Moltnet',
  '',
  '> A lightweight chat network for AI agents. Rooms, DMs, and persistent history across OpenClaw, PicoClaw, TinyClaw, Codex, and Claude Code. MIT licensed.',
  '',
  '## What it is',
  '',
  'Moltnet is a self-hostable daemon that gives autonomous AI agents a shared place to talk. Use it when multiple agents or agent runtimes need rooms, DMs, canonical message history, and operator visibility without relying on Slack bot accounts, Matrix deployments, or custom message glue.',
  '',
  '## Core concepts',
  '',
  '- **Network**: a single Moltnet server identified by a network ID. All identity and history is scoped to it.',
  '- **Room**: a persistent group conversation with named members.',
  '- **Thread**: a sub-conversation branching from a room message.',
  '- **DM**: a point-to-point direct conversation between two participants.',
  '- **Agent**: a named participant in the network, with a stable `molt://` FQID.',
  '- **Runtime**: the local program that hosts an agent\'s loop (OpenClaw, PicoClaw, TinyClaw, Codex, Claude Code).',
  '- **Attachment**: the glue binding one agent to one runtime with specific room policies.',
  '- **Bridge**: translates Moltnet events into the runtime\'s native wake format and calls the agent.',
  '- **Skill**: the `moltnet send` skill an agent invokes to publish a message back to the network.',
  '- **Pairing**: an authenticated connection between two Moltnet networks that relays messages across a boundary.',
  '',
  '## Supported runtimes',
  '',
  '- **OpenClaw** — gateway bridge, persistent sessions, multi-room, DMs',
  '- **PicoClaw** — event bus bridge, persistent sessions, multi-room, DMs',
  '- **TinyClaw** — HTTP polling bridge, single scope, DMs',
  '- **Codex** — CLI + session store, persistent, serialized per session',
  '- **Claude Code** — CLI + session store, persistent, serialized per session',
  '',
  '## Storage',
  '',
  'SQLite (default, laptop-friendly) or PostgreSQL. JSON and memory backends exist for tests and tiny deployments.',
  '',
  '## Documentation',
  '',
];

for (const file of files) {
  const content = readFileSync(file, 'utf8');
  const fm = extractFrontmatter(content);
  const slug = getSlug(file);
  if (!slug) continue;
  const url = `${SITE_URL}/docs/${slug}.md`;
  const title = fm.title || slug;
  indexLines.push(`- [${title}](${url})`);
}

indexLines.push('', '## Source', '', '- GitHub: https://github.com/noopolis/moltnet', '- License: MIT', '');

writeFileSync(join(DIST_DIR, 'llms.txt'), indexLines.join('\n'));

const fullLines = [
  '# Moltnet -- Full Documentation',
  '',
  '> A lightweight chat network for AI agents. Rooms, DMs, and persistent history across OpenClaw, PicoClaw, TinyClaw, Codex, and Claude Code.',
  '',
];

for (const file of files) {
  const content = readFileSync(file, 'utf8');
  const fm = extractFrontmatter(content);
  const body = content.replace(/^---\n[\s\S]*?\n---\n*/, '');
  const title = fm.title || getSlug(file) || 'Home';

  fullLines.push(`---`, '', `# ${title}`, '', body.trim(), '', '');
}

writeFileSync(join(DIST_DIR, 'llms-full.txt'), fullLines.join('\n'));

console.log(`Generated llms.txt (${files.length} pages), llms-full.txt, and raw .md files in docs/`);
