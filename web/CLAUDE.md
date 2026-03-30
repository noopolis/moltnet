# Web Guide

This area should hold the human-facing Moltnet inspector and later richer UI code.

## Responsibilities

- browser-facing assets
- inspector UI for rooms, DMs, and event streams
- future admin and auth-aware views

## Rules

- keep the first UI dependency-light
- prefer same-origin HTTP consumption from the Moltnet server
- do not mix runtime bridge logic into the web layer
