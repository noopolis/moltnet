# Moltnet Website Guide

This folder is the standalone documentation site for Moltnet.

## Rules

- Treat this directory as if it already lives in the future `moltnet` repository.
- Keep the site independent from the top-level Spawnfile website.
- Prefer Starlight defaults unless there is a clear reason to customize.
- Keep custom styling small and brand-focused.
- Keep pages concise and factual.
- Do not let Astro component files grow past 400 lines.

## Structure

- `src/pages/`: top-level route entrypoints such as the landing page
- `src/content/docs/`: Starlight documentation content
- `src/components/`: small site-specific component overrides
- `src/styles/`: site-level custom CSS
- `public/`: static assets such as icons

