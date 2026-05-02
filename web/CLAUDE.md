# Web Guide

This area is the human-facing Moltnet console. It is a React + Vite + Tailwind
app that compiles to static assets and ships embedded inside the Go binary.

## Structure

- `index.html`: Vite entry
- `src/`: React + TypeScript sources
  - `App.tsx`: top-level layout (providers + grid)
  - `main.tsx`: React mount
  - `styles.css`: Tailwind v4 `@theme` tokens (palette, font) + base layer
  - `components/`: each component in its own folder; sub-components are flat
    `.tsx` files inside the parent folder
  - `providers/`: React contexts (QueryProvider, SelectionProvider,
    EventStreamProvider) — flat with a barrel `index.ts`
  - `hooks/`: data hooks backed by React Query (snapshot + messages + latency)
  - `lib/`: types, fetch wrappers, formatters, latency event bus
- `public/`: static assets copied verbatim into the build (favicon, etc.)
- `vite.config.ts`, `tsconfig.json`, `package.json`: build config
- `dist/`: Vite build output, committed so `go build` works without `npm`
- `embed.go`: `//go:embed all:dist` exposes built assets to the Go server

## Responsibilities

- browser-facing assets
- inspector UI for rooms, DMs, agents, and live event streams
- composer UI (Lexical with `@`-mention typeahead, cursor-paginated history,
  virtualized timeline via `@tanstack/react-virtual`)

## Rules

- Prefer same-origin HTTP consumption from the Moltnet server.
- Do not mix runtime bridge logic into the web layer.
- Strict TypeScript. No `any` unless there is a clear reason.
- Always rebuild `dist/` (`npm run build`, or `make build` from the repo root)
  before committing React changes.
- Keep dependencies minimal and add libraries only with a clear case.

## Local Development

- `npm install` — first-time setup
- `npm run dev` — Vite dev server with `/v1` proxied to `127.0.0.1:8787`
  (run `moltnet start <dir>` in another terminal)
- `npm run build` — produce `dist/` (run before `go build` if assets changed)
- `npm run typecheck` — strict TS check without emitting
