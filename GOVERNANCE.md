# Governance

NorthScope is currently a maintainer-led pre-beta project.

## Principles

- Stay read-only by default.
- Optimize for Ingress traffic debugging, not full Kubernetes object visualization.
- Prefer simple topology views first, with deeper context available when needed.
- Keep installation boring and production-friendly through Helm.
- Avoid cluster agents, sidecars, CRDs, and mutating permissions unless there is a strong reason.

## Roles

### Maintainer

Maintainers can merge pull requests, cut releases, manage issues, update project direction, and moderate community spaces.

Current maintainers are listed in [MAINTAINERS.md](MAINTAINERS.md).

### Contributor

Contributors submit issues, pull requests, documentation improvements, test scenarios, screenshots, and design feedback.

## Decision Making

While NorthScope has a small maintainer set, decisions are made by maintainer consensus. If consensus is not possible, the current project owner makes the final call after documenting the tradeoff in the relevant issue or pull request.

Larger decisions should be discussed publicly before implementation when possible. Examples:

- Changes to the Kubernetes permission model
- New runtime dependencies or in-cluster components
- Breaking API, Helm, or UI workflow changes
- Gateway API becoming a first-class topology path

## Project Status

NorthScope is not a CNCF project. This governance document is inspired by common cloud native project practices and is intended to make project ownership and decision making clear from the beginning.
