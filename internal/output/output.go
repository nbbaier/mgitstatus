package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nbbaier/mgitstatus/internal/repo"
	"golang.org/x/term"
)

// ANSI color codes
const (
	cRed    = "\033[1;31m"
	cGreen  = "\033[1;32m"
	cYellow = "\033[1;33m"
	cBlue   = "\033[1;34m"
	cPurple = "\033[1;35m"
	cCyan   = "\033[1;36m"
	cReset  = "\033[0;10m"
)

// Color assignments matching the original script
var (
	cOK             = cGreen
	cLocked         = cRed
	cNeedsPush      = cYellow
	cNeedsPull      = cBlue
	cNeedsCommit    = cRed
	cNeedsUpstream  = cPurple
	cUntracked      = cCyan
	cStashes        = cYellow
	cUnsafe         = cPurple
)

// Formatter handles output formatting.
type Formatter struct {
	UseColor   bool
	Flatten    bool
	JSONOutput bool
	ShowBranch bool
	ExcludeOK  bool
}

// NewFormatter creates a formatter with color auto-detection.
func NewFormatter(forceColor, flatten, jsonOutput, showBranch, excludeOK bool) *Formatter {
	useColor := forceColor || term.IsTerminal(int(os.Stdout.Fd()))
	return &Formatter{
		UseColor:   useColor,
		Flatten:    flatten,
		JSONOutput: jsonOutput,
		ShowBranch: showBranch,
		ExcludeOK:  excludeOK,
	}
}

func (f *Formatter) color(c, text string) string {
	if !f.UseColor {
		return text
	}
	return c + text + cReset
}

// PrintError prints an error-status repo (unsafe_ownership, locked, not_a_repo).
func (f *Formatter) PrintError(path, errorType string) {
	if f.JSONOutput {
		obj := map[string]string{"path": path, "error": errorType}
		data, _ := json.Marshal(obj)
		fmt.Println(string(data))
		return
	}

	switch errorType {
	case "unsafe_ownership":
		fmt.Printf("%s: %s\n", path, f.color(cUnsafe, "Unsafe ownership, owned by someone else. Skipping."))
	case "locked":
		fmt.Printf("%s: %s\n", path, f.color(cLocked, "Locked. Skipping."))
	case "not_a_repo":
		fmt.Printf("%s: not a git repo\n", path)
	}
}

// PrintStatus prints the status for a repo.
func (f *Formatter) PrintStatus(s repo.Status) {
	if s.Error != "" {
		f.PrintError(s.Path, s.Error)
		return
	}

	if f.JSONOutput {
		f.printJSON(s)
		return
	}

	prefix := s.Path
	if f.ShowBranch && s.Branch != "" {
		prefix += " (" + s.Branch + ")"
	}

	var statuses []string

	if len(s.NeedsPushBranches) > 0 {
		text := fmt.Sprintf("Needs push (%s)", strings.Join(s.NeedsPushBranches, ","))
		status := f.color(cNeedsPush, text)
		statuses = append(statuses, status)
		if f.Flatten {
			fmt.Printf("%s: %s\n", prefix, status)
		}
	}

	if len(s.NeedsPullBranches) > 0 {
		text := fmt.Sprintf("Needs pull (%s)", strings.Join(s.NeedsPullBranches, ","))
		status := f.color(cNeedsPull, text)
		statuses = append(statuses, status)
		if f.Flatten {
			fmt.Printf("%s: %s\n", prefix, status)
		}
	}

	if len(s.NeedsUpstreamBranches) > 0 {
		text := fmt.Sprintf("Needs upstream (%s)", strings.Join(s.NeedsUpstreamBranches, ","))
		status := f.color(cNeedsUpstream, text)
		statuses = append(statuses, status)
		if f.Flatten {
			fmt.Printf("%s: %s\n", prefix, status)
		}
	}

	if s.Uncommitted {
		status := f.color(cNeedsCommit, "Uncommitted changes")
		statuses = append(statuses, status)
		if f.Flatten {
			fmt.Printf("%s: %s\n", prefix, status)
		}
	}

	if s.Untracked {
		status := f.color(cUntracked, "Untracked files")
		statuses = append(statuses, status)
		if f.Flatten {
			fmt.Printf("%s: %s\n", prefix, status)
		}
	}

	if s.Stashes > 0 {
		status := f.color(cStashes, fmt.Sprintf("%d stashes", s.Stashes))
		statuses = append(statuses, status)
		if f.Flatten {
			fmt.Printf("%s: %s\n", prefix, status)
		}
	}

	if len(statuses) == 0 {
		// OK
		if f.ExcludeOK {
			if f.Flatten {
				return
			}
			return
		}
		status := f.color(cOK, "ok")
		statuses = append(statuses, status)
		if f.Flatten {
			fmt.Printf("%s: %s\n", prefix, status)
		}
	}

	if !f.Flatten {
		if s.OK && f.ExcludeOK {
			return
		}
		fmt.Printf("%s: %s\n", prefix, strings.Join(statuses, " "))
	}
}

func (f *Formatter) printJSON(s repo.Status) {
	obj := map[string]interface{}{
		"path":           s.Path,
		"branch":         s.Branch,
		"ok":             s.OK,
		"needs_push":     toJSONArray(s.NeedsPushBranches),
		"needs_pull":     toJSONArray(s.NeedsPullBranches),
		"needs_upstream": toJSONArray(s.NeedsUpstreamBranches),
		"uncommitted":    s.Uncommitted,
		"untracked":      s.Untracked,
		"stashes":        s.Stashes,
	}

	if s.OK && f.ExcludeOK {
		return
	}

	data, _ := json.Marshal(obj)
	fmt.Println(string(data))
}

func toJSONArray(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
