package repo

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Status holds the results of all checks for a single repository.
type Status struct {
	Path                  string
	Branch                string
	NeedsPushBranches     []string
	NeedsPullBranches     []string
	NeedsUpstreamBranches []string
	Uncommitted           bool
	Untracked             bool
	Stashes               int
	OK                    bool
	Error                 string // "unsafe_ownership", "locked", "not_a_repo"
}

// Options controls which checks to run.
type Options struct {
	DoFetch      bool
	NoPush       bool
	NoPull       bool
	NoUpstream   bool
	NoUncommitted bool
	NoUntracked  bool
	NoStashes    bool
	Debug        bool
}

// CheckSafety checks ownership of .git dir against current user.
func CheckSafety(projDir string) error {
	gitDir := filepath.Join(projDir, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return nil // not a repo or can't stat, handled elsewhere
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil // can't get uid, skip check
	}

	currentUser, err := user.Current()
	if err != nil {
		return nil
	}

	currentUID, err := strconv.ParseUint(currentUser.Uid, 10, 32)
	if err != nil {
		return nil
	}

	if stat.Uid != uint32(currentUID) {
		return fmt.Errorf("unsafe_ownership")
	}
	return nil
}

// IsLocked checks if the repo has an index.lock file.
func IsLocked(projDir string) bool {
	lockFile := filepath.Join(projDir, ".git", "index.lock")
	_, err := os.Stat(lockFile)
	return err == nil
}

// ShouldIgnore checks the mgitstatus.ignore config option.
func ShouldIgnore(projDir string) bool {
	gitConf := filepath.Join(projDir, ".git", "config")
	out, err := exec.Command("git", "config", "-f", gitConf, "--bool", "mgitstatus.ignore").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// Check performs all status checks on a repo.
func Check(projDir string, opts Options) Status {
	gitDir := filepath.Join(projDir, ".git")
	workTree := projDir

	s := Status{Path: projDir}

	// Safety check
	if err := CheckSafety(projDir); err != nil {
		s.Error = "unsafe_ownership"
		return s
	}

	// Lock check
	if IsLocked(projDir) {
		s.Error = "locked"
		return s
	}

	// Fetch if requested
	if opts.DoFetch {
		cmd := exec.Command("git", "--work-tree", workTree, "--git-dir", gitDir, "fetch", "-q")
		cmd.Run()
	}

	// Refresh index
	cmd := exec.Command("git", "--work-tree", workTree, "--git-dir", gitDir, "update-index", "-q", "--refresh")
	cmd.Run()

	// Get current branch
	out, err := exec.Command("git", "--git-dir", gitDir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err == nil {
		s.Branch = strings.TrimSpace(string(out))
	}

	// Find all local branches by listing refs/heads
	branches := listLocalBranches(gitDir)

	if opts.Debug {
		fmt.Fprintf(os.Stderr, "DEBUG %s: branches=%v\n", projDir, branches)
	}

	for _, branch := range branches {
		// Check upstream
		upstream, err := gitOutput("git", "--git-dir", gitDir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", branch+"@{u}")
		if err != nil {
			// No upstream
			if !opts.NoUpstream {
				s.NeedsUpstreamBranches = appendUnique(s.NeedsUpstreamBranches, branch)
			}
			continue
		}

		// Count ahead/behind
		countOut, err := gitOutput("git", "--git-dir", gitDir, "rev-list", "--left-right", "--count", branch+"..."+upstream)
		if err != nil {
			continue
		}

		parts := strings.Fields(countOut)
		if len(parts) == 2 {
			ahead, _ := strconv.Atoi(parts[0])
			behind, _ := strconv.Atoi(parts[1])

			if opts.Debug {
				fmt.Fprintf(os.Stderr, "DEBUG %s: branch=%s ahead=%d behind=%d\n", projDir, branch, ahead, behind)
			}

			if ahead > 0 && !opts.NoPush {
				s.NeedsPushBranches = appendUnique(s.NeedsPushBranches, branch)
			}
			if behind > 0 && !opts.NoPull {
				s.NeedsPullBranches = appendUnique(s.NeedsPullBranches, branch)
			}
		}

		// Also check via merge-base for diverged branches
		revLocal, err1 := gitOutput("git", "--git-dir", gitDir, "rev-parse", "--verify", branch)
		revRemote, err2 := gitOutput("git", "--git-dir", gitDir, "rev-parse", "--verify", upstream)
		revBase, err3 := gitOutput("git", "--git-dir", gitDir, "merge-base", branch, upstream)

		if err1 == nil && err2 == nil && err3 == nil && revLocal != revRemote {
			if revLocal == revBase && !opts.NoPull {
				s.NeedsPullBranches = appendUnique(s.NeedsPullBranches, branch)
			}
			if revRemote == revBase && !opts.NoPush {
				s.NeedsPushBranches = appendUnique(s.NeedsPushBranches, branch)
			}
		}
	}

	// Uncommitted changes (unstaged + uncommitted)
	if !opts.NoUncommitted {
		cmd1 := exec.Command("git", "--work-tree", workTree, "--git-dir", gitDir, "diff-index", "--quiet", "HEAD", "--")
		err1 := cmd1.Run()
		cmd2 := exec.Command("git", "--work-tree", workTree, "--git-dir", gitDir, "diff-files", "--quiet", "--ignore-submodules", "--")
		err2 := cmd2.Run()
		if err1 != nil || err2 != nil {
			s.Uncommitted = true
		}
	}

	// Untracked files
	if !opts.NoUntracked {
		out, err := exec.Command("git", "--work-tree", workTree, "--git-dir", gitDir, "ls-files", "--exclude-standard", "--others").Output()
		if err == nil && len(strings.TrimSpace(string(out))) > 0 {
			s.Untracked = true
		}
	}

	// Stashes
	if !opts.NoStashes {
		cmd := exec.Command("git", "--git-dir", gitDir, "stash", "list")
		cmd.Dir = workTree
		out, err := cmd.Output()
		if err == nil {
			lines := strings.TrimSpace(string(out))
			if lines != "" {
				s.Stashes = len(strings.Split(lines, "\n"))
			}
		}
	}

	// Determine if OK
	s.OK = len(s.NeedsPushBranches) == 0 &&
		len(s.NeedsPullBranches) == 0 &&
		len(s.NeedsUpstreamBranches) == 0 &&
		!s.Uncommitted &&
		!s.Untracked &&
		s.Stashes == 0

	if opts.Debug {
		fmt.Fprintf(os.Stderr, "DEBUG %s: status=%+v\n", projDir, s)
	}

	return s
}

func listLocalBranches(gitDir string) []string {
	refsDir := filepath.Join(gitDir, "refs", "heads")
	var branches []string

	filepath.Walk(refsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(refsDir, path)
		if err != nil {
			return nil
		}
		branches = append(branches, rel)
		return nil
	})

	// Also check packed-refs for branches not in refs/heads/
	packedRefsPath := filepath.Join(gitDir, "packed-refs")
	data, err := os.ReadFile(packedRefsPath)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") || line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ref := parts[1]
				const prefix = "refs/heads/"
				if strings.HasPrefix(ref, prefix) {
					branch := ref[len(prefix):]
					branches = appendUnique(branches, branch)
				}
			}
		}
	}

	return branches
}

func gitOutput(args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}
