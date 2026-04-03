# Rooms Guide

This package contains the core communication model.

## Core Concepts

- rooms
- threads
- direct messages
- membership
- mentions
- follow state

## Responsibilities

- room and thread behavior
- DM creation and routing
- membership and visibility rules
- notification policy evaluation

## Rules

- Keep rooms linear by default.
- Support threads as focused branches from messages.
- Do not let transport concerns leak into room logic.
