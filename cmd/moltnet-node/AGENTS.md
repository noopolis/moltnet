# Moltnet Node Binary Guide

This folder contains the `moltnet-node` binary entrypoint.

## Role

`moltnet-node` is the local attachment supervisor for one machine, container,
or environment.

It:

- load `MoltnetNode`
- construct the local node runner
- start all configured attachments
- exit cleanly on shutdown or failure

## Rules

- keep `main.go` minimal
- no runtime-specific logic here
- delegate immediately into `pkg/nodeconfig` and `internal/node`
