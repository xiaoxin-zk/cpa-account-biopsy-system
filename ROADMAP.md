# Roadmap

## Current State

Already in place:

- standalone Docker deployment
- unified install / update / uninstall entrypoint
- independent dashboard auth
- account health report and manual probing
- quota window display
- runtime health fallback via management API
- probe feedback and stale quota snapshot preservation
- dirty-worktree warning during updates

## Current Priorities

1. Make server-side probe results more explainable.
2. Improve automatic disable / recover behavior for quota and blocked states.
3. Tighten server verification and regression coverage.
4. Improve observability for failed probes and management API fallbacks.
5. Keep install and update behavior predictable on real servers.

## Good First Issue

Suitable starter tasks:

- improve wording in README and UI messages
- add tests around edge-case probe states
- improve error messages in scripts
- document common deployment layouts
- add screenshots or UI walkthrough docs

## Help Wanted

Useful areas for community help:

- reproducing server-side edge cases
- improving frontend state explanations
- adding regression tests for quota / blocked / team accounts
- reviewing install and update safety on more environments
- improving issue triage and docs maintenance

## Likely Next Milestones

1. Better structured probe logs per account.
2. More explicit account action history in the dashboard.
3. Safer update / rollback operations.
4. Better docs for contributors and deployers.
5. More targeted automated tests for real-world management API payloads.
