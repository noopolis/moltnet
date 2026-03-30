# Bridge Config Guide

This package should define the public bridge configuration schema.

## Purpose

Spawnfile should eventually compile these config files.
The Moltnet bridge should consume them directly.

## Rules

- keep config types transport-neutral where possible
- use explicit version fields
- keep runtime adapter sections narrow and declarative
- design the package so external tooling can validate configs too
