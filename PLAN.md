# Rewrite Plan: `mgitstatus` in Go

## Why Go

Go is ideal here — easy concurrency via goroutines (the #1 speed win), fast single binary, and shelling out to `git` or using `go-git` are both viable.

## Proposed Structure

```
cmd/mgitstatus/main.go   — CLI entry point, flag parsing, output
internal/
  scanner/scanner.go      — directory walking (find repos)
  repo/repo.go            — per-repo status checks (all 6)
  output/output.go        — formatting, color, flatten/normal modes
```

## Implementation Phases

### Phase 1 — CLI & Directory Walking

- Parse all flags (use `pflag` or stdlib `flag`) mirroring the original's `-b`, `-c`, `-d`, `-e`, `-f`, `-w`, `--flatten`, `--json`, `--no-push/pull/upstream/uncommitted/untracked/stashes`, `--throttle`, `--version`
- Walk directories with `filepath.WalkDir`, respecting `--depth` / `--no-depth` and following symlinks (`-L` behavior)
- Detect `.git/` dirs to identify repos

### Phase 2 — Per-Repo Status Checks

Implement all 6 checks by shelling out to `git` (simplest, most compatible):

1. **Needs push** — `git rev-list --left-right --count branch...upstream`
2. **Needs pull** — same command, check behind count
3. **Needs upstream** — `git rev-parse --abbrev-ref @{u}` fails → no upstream
4. **Uncommitted changes** — `git diff-index --quiet HEAD` + `git diff-files --quiet`
5. **Untracked files** — `git ls-files --exclude-standard --others`
6. **Stashes** — `git stash list | wc -l`

Plus safety checks:

- **Ownership validation** — compare `.git` dir owner against current user
- **`index.lock` detection** — skip repos with an active lock file
- **`mgitstatus.ignore` config** — read `git config --local mgitstatus.ignore` and skip if `true`

### Phase 3 — Concurrency (the big speed win)

- Use a worker pool of goroutines (e.g., `N=runtime.NumCPU()`) to check repos in parallel
- Collect results into a channel, print in discovery order to preserve deterministic output
- Optional `-f` fetch can also be parallelized (with `--throttle` as a rate limiter)

### Phase 4 — Output

- ANSI color (auto-detect TTY via `os.Stdout.Fd()` + `isatty`, force with `-c`)
- Normal mode (one line per repo) and `--flatten` (one line per status)
- `--json` mode — JSONL output (one JSON object per repo per line), with fields:
  - `path`, `branch`, `ok` (bool), `needs_push` / `needs_pull` / `needs_upstream` (arrays of branch names), `uncommitted` (bool), `untracked` (bool), `stashes` (int)
  - Error cases emit `{"path":"...","error":"unsafe_ownership|not_a_repo|locked"}`
  - Respects `--no-*` filters and `-e`/`--no-ok` exclusion
  - Use `encoding/json` for proper escaping (replaces the shell `json_escape` function)
- Branch display (`-b`), `--no-ok` filtering
- Match the original color scheme:

| Status             | Color        | ANSI           |
| ------------------ | ------------ | -------------- |
| `ok`               | Bold green   | `\033[1;32m`   |
| `Uncommitted`      | Bold red     | `\033[1;31m`   |
| `Needs push`       | Bold yellow  | `\033[1;33m`   |
| `Needs pull`       | Bold blue    | `\033[1;34m`   |
| `Needs upstream`   | Bold magenta | `\033[1;35m`   |
| `Untracked files`  | Bold cyan    | `\033[1;36m`   |
| `Stashes`          | Bold yellow  | `\033[1;33m`   |
| `Locked`           | Bold red     | `\033[1;31m`   |
| `Unsafe ownership` | Bold magenta | `\033[1;35m`   |

### Phase 5 — Polish

- `MG_DEBUG` env var support for verbose debug output
- Edge cases:
  - Repos with no commits (no HEAD ref)
  - Branch names with `/` (e.g., `feature/foo`)
  - Symlinked repos and directories
  - `--no-depth` vs `-d 0` mutual exclusivity
- Integration tests comparing output against the original script

## Key Speed Advantages Over Bash

1. **Parallel repo checks** — biggest win, especially with many repos
2. **No fork/exec per check** — can batch git commands or use `go-git` for in-process checks later
3. **Single binary** — no shell startup overhead

## Original Feature Reference

### All CLI Flags

| Flag                 | Default | Description                                              |
| -------------------- | ------- | -------------------------------------------------------- |
| `-b`                 | off     | Show currently checked-out branch name next to repo path |
| `-c`                 | off     | Force color output even when piping                      |
| `-d N` / `--depth=N` | `2`     | Max directory depth; `0` = infinite                      |
| `--no-depth`         | off     | Don't recurse at all                                     |
| `-e` / `--no-ok`     | off     | Exclude repos with `ok` status                           |
| `-f`                 | off     | Run `git fetch -q` before checking                       |
| `--throttle SEC`     | `0`     | Sleep between repos (only with `-f`)                     |
| `--flatten`          | off     | One status per output line                               |
| `--json`             | off     | Output each repo as a JSON object (JSONL, one per line)  |
| `-w`                 | off     | Warn about non-git-repo directories                      |
| `--no-push`          | off     | Suppress "Needs push"                                    |
| `--no-pull`          | off     | Suppress "Needs pull"                                    |
| `--no-upstream`      | off     | Suppress "Needs upstream"                                |
| `--no-uncommitted`   | off     | Suppress "Uncommitted changes"                           |
| `--no-untracked`     | off     | Suppress "Untracked files"                               |
| `--no-stashes`       | off     | Suppress stash count                                     |
| `--version`          | —       | Print version and exit                                   |
| `-h` / `--help`      | —       | Print usage and exit                                     |
