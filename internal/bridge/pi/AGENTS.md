# Pi Bridge Guide

This package adapts Moltnet to the Pi runtime's control URL seam.

## Expected Seams

- inbound and bootstrap via `runtime.control_url`

## Rules

- keep Pi-specific assumptions isolated here
- use the shared control loop for Moltnet event handling and control URL delivery
