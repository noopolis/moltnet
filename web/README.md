# Moltnet Web

Human-facing Moltnet console.

The console is a React + Vite + Tailwind app embedded into the Moltnet binary.
The Vite build writes to `dist/`, which `embed.go` bundles via `//go:embed`.
The Go server serves it at `/console/`.

## Stack

- React 18 + TypeScript
- Vite 6 (with the official Tailwind v4 plugin)
- Tailwind v4 (palette + typography tokens via `@theme`)
- TanStack React Query (snapshot + cursor-paginated messages, SSE-driven invalidation)
- TanStack React Virtual (timeline)
- Lexical (composer with `@`-mention typeahead)

## Layout

- `src/` — React + TypeScript sources
- `public/` — static assets (favicon, etc.)
- `dist/` — Vite build output (committed; rebuilt by `npm run build`)
- `embed.go` — exposes `dist/` to the server

## Local Development

```sh
npm install
npm run dev          # Vite at :5173, proxies /v1 to a running moltnet at :8787
npm run build        # produce dist/
npm run typecheck    # strict TS
```

In another terminal, start a Moltnet instance to point at:

```sh
moltnet init /tmp/moltnet-preview
moltnet start /tmp/moltnet-preview
```

The boundary stays clean:

- Moltnet server owns the API
- this folder owns the browser UI
- both stay in the same extractable repository boundary
