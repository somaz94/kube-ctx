# Development

Guide for building, testing, and contributing to this project.

<br/>

## Prerequisites

- Go 1.26+
- Make

<br/>

## Build

```bash
make build           # Build binary → ./bin/kctx
make clean           # Remove build artifacts
make install         # Install to /usr/local/bin
```

<br/>

## Testing

```bash
make test            # Run unit tests (alias)
make test-unit       # go test ./... -v -race -cover
make cover           # Generate coverage report
make cover-html      # Open coverage report in browser
```

<br/>

## Code Quality

```bash
make fmt             # Format code (go fmt)
make vet             # Run go vet
```

<br/>

## Workflow

```bash
make check-gh        # Verify gh CLI is installed and authenticated
make branch name=feature-name   # Create feature branch from main
make pr title="feat: add feature"   # Test → push → create PR (auto-generates body)
```

`make pr` automatically:
1. Runs `go test ./... -race -cover` and `go vet`
2. Pushes the branch to origin
3. Generates PR body from commit history (categorized by `feat:`, `fix:`, `test:`, `docs:`)
4. Detects changed test packages and builds a test plan checklist
5. Creates the PR via `gh pr create`

<br/>

## CI/CD Workflows

| Workflow | Trigger | Description |
|----------|---------|-------------|
| `ci.yml` | push (main), PR, dispatch | Unit tests → Build → Version verify |
| `release.yml` | tag push `v*` | GoReleaser (binaries + Homebrew + Scoop) |
| `changelog-generator.yml` | after release, PR merge | Auto-generate CHANGELOG.md |
| `contributors.yml` | after changelog | Auto-generate CONTRIBUTORS.md |
| `stale-issues.yml` | daily cron | Auto-close stale issues |
| `dependabot-auto-merge.yml` | PR (dependabot) | Auto-merge minor/patch updates |
| `issue-greeting.yml` | issue opened | Welcome message |

### Workflow Chain

```
tag push v* → Create release (GoReleaser)
                └→ Generate changelog
                      └→ Generate Contributors
```

<br/>

## Conventions

- **Commits**: Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `ci:`, `chore:`)
- **Secrets**: `PAT_TOKEN` (cross-repo ops), `GITHUB_TOKEN` (releases)
- **paths-ignore**: `.github/workflows/**`, `**/*.md`
