package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/nbbaier/mgitstatus/internal/output"
	"github.com/nbbaier/mgitstatus/internal/repo"
	"github.com/nbbaier/mgitstatus/internal/scanner"
)

const version = "2.3"

func main() {
	// CLI flags
	showBranch := flag.Bool("b", false, "Show currently checked out branch")
	forceColor := flag.Bool("c", false, "Force color output (preserve colors when using pipes)")
	depth := flag.Int("d", 2, "Scan this many directories deep (0 = infinite)")
	excludeOK := flag.Bool("e", false, "Exclude repos that are 'ok'")
	doFetch := flag.Bool("f", false, "Do a 'git fetch' on each repo (slow for many repos)")
	warnNotRepo := flag.Bool("w", false, "Warn about dirs that are not Git repositories")
	flatten := flag.Bool("flatten", false, "Show only one status per line")
	jsonOutput := flag.Bool("json", false, "Output each repo as a JSON object (one per line, JSONL)")
	noDepth := flag.Bool("no-depth", false, "Do not recurse into directories")
	noOK := flag.Bool("no-ok", false, "Exclude repos that are 'ok' (same as -e)")
	noPush := flag.Bool("no-push", false, "Suppress 'Needs push'")
	noPull := flag.Bool("no-pull", false, "Suppress 'Needs pull'")
	noUpstream := flag.Bool("no-upstream", false, "Suppress 'Needs upstream'")
	noUncommitted := flag.Bool("no-uncommitted", false, "Suppress 'Uncommitted changes'")
	noUntracked := flag.Bool("no-untracked", false, "Suppress 'Untracked files'")
	noStashes := flag.Bool("no-stashes", false, "Suppress stash count")
	throttle := flag.Int("throttle", 0, "Wait SEC seconds between each 'git fetch' (-f option)")
	showVersion := flag.Bool("version", false, "Show version")

	// Override default usage
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `
Usage: %s [--version] [-w] [-e] [-f] [--throttle SEC] [-c] [-d/--depth=2] [--flatten] [--json] [--no-X] [DIR [DIR]...]

mgitstatus shows uncommitted, untracked and unpushed changes in multiple Git
repositories. By default, mgitstatus scans two directories deep. This can be
changed with the -d (--depth) option.  If DEPTH is 0, the scan is infinitely
deep.

  -b               Show currently checked out branch
  -c               Force color output (preserve colors when using pipes)
  -d, --depth=2    Scan this many directories deep
  -e               Exclude repos that are 'ok'
  -f               Do a 'git fetch' on each repo (slow for many repos)
  -h, --help       Show this help message
  --flatten        Show only one status per line
  --json           Output each repo as a JSON object (one per line, JSONL)
  --no-depth       Do not recurse into directories (incompatible with -d)
  --throttle SEC   Wait SEC seconds between each 'git fetch' (-f option)
  --version        Show version
  -w               Warn about dirs that are not Git repositories

You can limit output with the following options:

  --no-push
  --no-pull
  --no-upstream
  --no-uncommitted
  --no-untracked
  --no-stashes
  --no-ok          (same as -e)
`, os.Args[0])
	}

	// Also support --depth=N syntax
	flag.IntVar(depth, "depth", 2, "Scan this many directories deep (0 = infinite)")

	flag.Parse()

	if *showVersion {
		fmt.Printf("v%s\n", version)
		os.Exit(0)
	}

	if *noOK {
		*excludeOK = true
	}

	debug := os.Getenv("MG_DEBUG") == "1"

	// Directories to scan
	dirs := flag.Args()
	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	// Build options
	opts := repo.Options{
		DoFetch:       *doFetch,
		NoPush:        *noPush,
		NoPull:        *noPull,
		NoUpstream:    *noUpstream,
		NoUncommitted: *noUncommitted,
		NoUntracked:   *noUntracked,
		NoStashes:     *noStashes,
		Debug:         debug,
	}

	// Build formatter
	formatter := output.NewFormatter(*forceColor, *flatten, *jsonOutput, *showBranch, *excludeOK)

	// Find all directories
	entries := scanner.FindAllDirs(dirs, *depth, *noDepth)

	if debug {
		fmt.Fprintf(os.Stderr, "DEBUG: found %d directories\n", len(entries))
	}

	// Filter to repos and non-repos, preserving discovery order
	type indexedResult struct {
		index  int
		status repo.Status
		isRepo bool
		path   string
	}

	results := make([]indexedResult, len(entries))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())

	for i, entry := range entries {
		if !entry.IsRepo {
			results[i] = indexedResult{
				index:  i,
				isRepo: false,
				path:   entry.Path,
			}
			continue
		}

		// Check if repo should be ignored
		if repo.ShouldIgnore(entry.Path) {
			results[i] = indexedResult{
				index:  i,
				isRepo: false, // treat as skip
				path:   "",    // empty path = skip silently
			}
			continue
		}

		wg.Add(1)
		go func(idx int, path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if *doFetch && *throttle > 0 {
				time.Sleep(time.Duration(*throttle) * time.Second)
			}

			s := repo.Check(path, opts)
			results[idx] = indexedResult{
				index:  idx,
				status: s,
				isRepo: true,
				path:   path,
			}
		}(i, entry.Path)
	}

	wg.Wait()

	// Print results in discovery order
	for _, r := range results {
		if r.path == "" {
			continue // silently skipped (ignored repo)
		}

		if !r.isRepo {
			if *warnNotRepo && r.path != "." {
				formatter.PrintError(r.path, "not_a_repo")
			}
			continue
		}

		formatter.PrintStatus(r.status)
	}
}
