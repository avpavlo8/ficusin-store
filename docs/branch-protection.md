# Main branch protection

Owner: Platform
Last reviewed: 2026-09-07

`main` is the production release branch. Direct pushes, force pushes and branch
deletion are prohibited. Configure a GitHub ruleset that applies to `main` with
these controls:

- require a pull request and at least one approval;
- dismiss stale approvals when new commits are pushed;
- require all review conversations to be resolved;
- require linear history and block force pushes and deletion;
- apply the rule to administrators without a routine bypass;
- require these pull-request checks: `frontend`, `layout`, `backend`,
  `migrations`, `saby-sync-tests`, `image`, `dependency-review`, CodeQL Go and
  CodeQL JavaScript/TypeScript analysis.

Use a time-limited, audited emergency bypass only for an active incident. The
incident record must name the approver, commit, reason and follow-up pull request.

Before adding or renaming a required check, confirm its exact current context on
a green pull request. A required context that no workflow emits blocks every
release.
