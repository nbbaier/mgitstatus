package scanner

import (
	"os"
	"path/filepath"
	"strings"
)

// FindRepos walks the given directories and returns paths to directories
// containing a .git subdirectory. It respects maxDepth (0 = infinite) and
// follows symlinks.
func FindRepos(dirs []string, maxDepth int, noDepth bool) []string {
	var repos []string
	for _, dir := range dirs {
		repos = append(repos, findReposInDir(dir, maxDepth, noDepth)...)
	}
	return repos
}

// DirEntry represents a discovered directory that may or may not be a repo.
type DirEntry struct {
	Path  string
	IsRepo bool
}

// FindAllDirs walks the given directories and returns all directories found,
// annotated with whether they contain a .git subdirectory.
func FindAllDirs(dirs []string, maxDepth int, noDepth bool) []DirEntry {
	var entries []DirEntry
	for _, dir := range dirs {
		entries = append(entries, findAllDirsInDir(dir, maxDepth, noDepth)...)
	}
	return entries
}

func findAllDirsInDir(dir string, maxDepth int, noDepth bool) []DirEntry {
	var entries []DirEntry

	// Resolve symlinks for the base dir
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return entries
	}

	baseDepth := countPathComponents(resolved)

	filepath.Walk(resolved, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Follow symlinks: if it's a symlink, resolve and check if dir
		if info.Mode()&os.ModeSymlink != 0 {
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil
			}
			realInfo, err := os.Stat(realPath)
			if err != nil {
				return nil
			}
			if !realInfo.IsDir() {
				return nil
			}
			// Recurse into symlinked directories
			subEntries := findAllDirsInDir(realPath, maxDepth, noDepth)
			entries = append(entries, subEntries...)
			return filepath.SkipDir
		}

		if !info.IsDir() {
			return nil
		}

		currentDepth := countPathComponents(path) - baseDepth

		// Handle depth limits
		if noDepth && currentDepth > 0 {
			return filepath.SkipDir
		}
		if maxDepth > 0 && currentDepth > maxDepth {
			return filepath.SkipDir
		}

		// Check if this directory is a git repo
		gitDir := filepath.Join(path, ".git")
		gitInfo, err := os.Stat(gitDir)
		isRepo := err == nil && gitInfo.IsDir()

		// Use the original dir prefix for display
		displayPath := path
		if resolved != dir {
			rel, err := filepath.Rel(resolved, path)
			if err == nil {
				displayPath = filepath.Join(dir, rel)
			}
		}

		entries = append(entries, DirEntry{Path: displayPath, IsRepo: isRepo})

		return nil
	})

	return entries
}

func findReposInDir(dir string, maxDepth int, noDepth bool) []string {
	var repos []string

	entries := findAllDirsInDir(dir, maxDepth, noDepth)
	for _, e := range entries {
		if e.IsRepo {
			repos = append(repos, e.Path)
		}
	}

	return repos
}

func countPathComponents(path string) int {
	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == "/" {
		return 0
	}
	return len(strings.Split(cleaned, string(filepath.Separator)))
}
