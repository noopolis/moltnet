# Workflow Guide

This folder contains GitHub Actions workflows for Moltnet.

## Rules

- Release workflows must publish the exact asset names consumed by `website/public/install.sh`.
- Keep triggers narrow and intentional.
- Pin third-party GitHub Actions to immutable SHAs and keep the workflow readable enough to debug from logs.
- Prefer a build job plus a publish job over racing many matrix jobs directly against one release.
