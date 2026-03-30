# Cmd Guide

This folder holds Moltnet binary entrypoints.

## Rules

- Each subfolder is one executable.
- Keep entrypoints thin.
- Parse config, construct the app, start it, and exit cleanly on failure.
- Do not place domain logic here.

## Expected Growth

- `moltnet/`: main server daemon
- `moltnet-bridge/`: runtime bridge daemon
- future admin or migration commands can live in separate subfolders
