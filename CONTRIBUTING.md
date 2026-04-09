# Contributing to CPA Account Biopsy System

Thanks for your interest in improving CPA Account Biopsy System.

This project welcomes community contributions of different sizes, including bug reports, feature suggestions, documentation fixes, tests, UI polish, deployment improvements, and code changes.

## Before You Start

1. Read the `README.md` for project context and current scope.
2. Check existing Issues and Pull Requests to avoid duplicate work.
3. For larger changes, open an Issue first so the direction can be aligned before implementation.

## Ways to Contribute

You can help by:

- Reporting bugs
- Suggesting features
- Improving docs and examples
- Adding or updating tests
- Fixing backend logic
- Improving frontend clarity and usability
- Improving install/update diagnostics
- Reviewing PRs and reproducing bugs

## Development Flow

1. Fork the repository.
2. Create a focused branch from `main`.
3. Make the smallest correct change for the task.
4. Run the relevant checks before opening a PR.
5. Open a Pull Request with a clear summary and verification notes.

## Pull Request Expectations

Please keep PRs focused and explain:

- what problem is being solved
- why the change is needed
- how it was verified
- whether there are any known risks or follow-up tasks

Good PRs usually include:

- a concise title
- a short summary
- test or verification notes
- screenshots for UI changes when useful

## Checks Before Opening a PR

Run what applies to your change.

For Go backend changes:

```bash
go test ./...
```

For script changes:

- validate shell syntax where possible
- describe how you tested the script path

For UI changes:

- confirm the page still loads
- verify desktop and mobile layouts if the change affects rendering

## Style Expectations

- Prefer minimal changes over wide refactors.
- Keep behavior explicit and easy to inspect.
- Avoid unrelated edits in the same PR.
- Do not commit secrets, `.env` files, account files, or tokens.
- Match the existing code and documentation style.

## Good First Contributions

These are especially welcome:

- wording and documentation fixes
- test coverage improvements
- clearer error messages
- small UI clarity improvements
- issue reproduction notes

## Need Ideas?

Look for Issues labeled:

- `good first issue`
- `help wanted`
- `bug`
- `documentation`

If there are no labeled Issues yet, feel free to open one and ask where help is most useful.
