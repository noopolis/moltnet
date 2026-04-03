# Node Guide

This package wires the local `moltnet-node` process.

## Responsibilities

- load many local agent attachments into one process
- construct one runtime runner per attachment
- supervise attachment lifecycles together

## Non-Responsibilities

- no protocol ownership
- no Moltnet server HTTP handlers
- no runtime-specific adapter details

## Rules

- keep supervision logic small and testable
- treat each attachment as an isolated runner inside one node
- cancel sibling attachments when one fails
