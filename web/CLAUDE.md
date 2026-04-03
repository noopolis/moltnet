# Web Guide

This area holds the human-facing Moltnet console and related browser assets.

## Responsibilities

- browser-facing assets
- inspector UI for rooms, DMs, and event streams
- admin and auth-aware console views when they live in the embedded UI

## Rules

- keep the first UI dependency-light
- prefer same-origin HTTP consumption from the Moltnet server
- do not mix runtime bridge logic into the web layer
