# Keeping private data out of Git

Only application source, tests, dependency manifests and public documentation
belong in this repository. Keep catalogue data, borrower information, covers,
backups, credentials and machine-specific configuration outside Git.

The ignore file excludes common data and credential paths, generated output,
and local development screenshots and notes. Ignore rules do not protect secrets
embedded in source or files that are already tracked.

## Before committing

Install [Gitleaks](https://github.com/gitleaks/gitleaks), then enable the supplied
local hook in each development clone:

```sh
git config core.hooksPath .githooks
```

The hook scans staged changes and blocks commits if Gitleaks finds a secret or
is unavailable. An ignored `bin/gitleaks` executable or the `GITLEAKS_BIN`
environment variable can select a local installation. Hooks are local safeguards;
they can be bypassed, and scanners do not detect every possible secret.

Before publishing a release, also scan the complete history:

```sh
gitleaks git --redact --no-banner .
```

Review the selected files for private information as well as credentials.
If a real credential is published, revoke or rotate it immediately; deleting
it in a later commit does not remove it from history.

## Deployment

Keep the database and backups outside the application checkout. The default
service listens on loopback. Review access controls before exposing the
application on a network; a public source repository does not make household
catalogue data public.

## Remote application updates

The HTTPS gateway checks the owner login and request origin. It overwrites the
owner identity passed to a private Unix socket; the controller also checks the
connecting account. The browser submits only a release version and request ID,
not a URL, command or filesystem path.

Downloads run without administrator privileges. A fixed root-owned activation
helper independently verifies publisher signatures, hashes, architecture, schema
compatibility and archive paths. Release code runs as restricted application or
smoke-test accounts. Data, TLS keys, login hashes and updater configuration stay
outside signed application releases. See [the update guide](UPDATES_README.md).

Repository owners and anyone able to change its release workflow or signing
secret remain trusted publishers. Protect GitHub access and review workflow
changes. Source scans are safeguards, not proof that every possible secret or
vulnerability is absent.
