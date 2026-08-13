# Pairings Guide

This package owns remote Moltnet pairing discovery and relay helpers.

## Responsibilities

- fetch remote network metadata
- fetch remote room listings
- fetch remote agent listings
- keep remote namespaces explicit

## Non-Responsibilities

- no local room/message storage
- no UI formatting

## Rules

- keep discovery and relay logic explicit
- use explicit remote base URLs from pairing config
- do not pretend paired networks share one flat namespace
