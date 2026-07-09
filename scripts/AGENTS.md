# Scripts Working Guide

This folder contains repository helper scripts used by Makefile targets and local operator workflows.

Rules:
- Keep scripts small and explicit.
- Prefer failing early with clear prerequisites.
- Do not store credentials in scripts or generated images. Read them from the environment or mount them at run time.
- Scripts that launch paid or credentialed external systems must be opt-in and documented.
