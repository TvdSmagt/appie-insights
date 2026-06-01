# Contributing

## Commit style

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add weekly CO₂ comparison chart
fix: handle missing product weight in enrichment
refactor: simplify keyword matching logic
doc: update getting started instructions
test: add classifier edge-case coverage
```

Common types: `feat`, `fix`, `refactor`, `doc`, `test`, `chore`.

## Development setup

**Prerequisites:** Docker, Docker Compose, Python 3.11+, Go 1.22+.

Start the services:

```bash
./run.sh
```

On first run the dashboard will prompt you to log in via the browser OAuth flow.

For Python development, create a virtual environment in the relevant service directory and install `requirements.txt`.

## Testing

Tests cover the Python enrichment and dashboard code. Run them before opening a PR:

```bash
./test.sh              # fast unit tests (default)
./test.sh unit         # all unit tests, including slow classifier tests
./test.sh integration  # integration tests (requires AH credentials)
./test.sh all          # everything
```

The integration tests make live AH API calls and need real credentials. If you
run them outside of Docker (so the `appie-config` named volume isn't available),
log in once first to write a token to `~/.config/appie/appie.json`:

```bash
make test-login        # opens your browser for the AH OAuth flow
```

The integration tests pick up that token automatically. Alternatively, set
`AH_ACCESS_TOKEN` / `AH_REFRESH_TOKEN` in your environment.

New enrichment logic, CO₂ mappings, or scoring changes should come with tests. The `tests/` directory mirrors the source layout; add tests in the matching subdirectory.

Slow tests (those that download or use the MiniLM model) must be marked with `@pytest.mark.slow` so they are excluded from the fast suite.

## Pull requests

- Keep PRs focused — one logical change per PR.
- Describe *why* the change is needed, not just what it does.
- All tests must pass (`make test-all` or `./test.sh all`) before requesting review.

## Releases

Versions follow [Semantic Versioning](https://semver.org/) and are tagged as `vX.Y.Z`.

To cut a release:

1. Update `CHANGELOG.md` with the new version and date.
2. Tag the commit: `git tag vX.Y.Z && git push --tags`.

`./run.sh` resolves the version automatically and passes it to the build: it uses
the git tag when one exists (e.g. `v1.0.0`), falls back to `prerelease+<commit>`
before the first tag, and to `development` when git is unavailable. The value is
forwarded to the Go build via the `VERSION` build arg (`-ldflags`), so
`GET /version` and the dashboard sidebar report the resolved version.

To stamp a manual `docker compose build`, set the arg yourself:

```bash
VERSION=$(git describe --tags) docker compose build
```

## Project structure

| Directory     | Language | Notes                                                                       |
| ------------- | -------- | --------------------------------------------------------------------------- |
| `backend/`    | Go       | HTTP API, OAuth login, receipt/order sync, CO₂eq enrichment, analytics     |
| `dashboard/`  | Python   | Streamlit visualisation, corrections, and settings UI                       |
| `tests/`      | Python   | Test suite for the Python dashboard and enrichment code                     |
| `docs/`       | —        | Architecture overview and API spec                                          |

## Privacy

No grocery data, credentials, or tokens should ever be committed. `config/appie.json` and `data/groceries.db` are gitignored — keep it that way.
