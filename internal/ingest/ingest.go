// Package ingest discovers JUnit XML files, groups them into runs, and
// orders the runs chronologically.
//
// A "run" is one CI execution of the whole suite. Two grouping modes cover
// the common artifact layouts:
//
//   - file (default): every XML file is its own run — the layout produced
//     by "download the report of each pipeline into one folder".
//   - dir: every directory is one run and all XML files inside it belong
//     to that run — the layout produced by "one artifact folder per
//     pipeline, one file per shard/module".
//
// Runs are ordered by the earliest <testsuite timestamp="…"> they contain;
// runs without any timestamp sort first among themselves by path, which
// keeps zero-padded run folders (run-001, run-002, …) chronological.
package ingest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JaydenCJ/flakesift/internal/junit"
)

// Run is one grouped CI execution.
type Run struct {
	ID    string    // grouping key: file path, or directory path in dir mode
	Time  time.Time // earliest suite timestamp; zero when none present
	Files []string  // source files, sorted
	Cases []junit.Case
}

// Options controls discovery and grouping.
type Options struct {
	// GroupByDir treats each directory as one run instead of each file.
	GroupByDir bool
}

// Result is the outcome of collection, including files that were
// discovered but skipped because they are not JUnit XML.
type Result struct {
	Runs    []Run
	Skipped []string // discovered .xml files whose root element is foreign
}

// Collect expands args (files and directories) and builds ordered runs.
// Explicitly named files must parse; files discovered by walking a
// directory are skipped when their root element is not JUnit.
func Collect(args []string, opt Options) (*Result, error) {
	files, discovered, err := expand(args)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no JUnit XML files found under %s", strings.Join(args, ", "))
	}

	res := &Result{}
	groups := make(map[string]*Run)
	var order []string
	for _, f := range files {
		rep, err := junit.ParseFile(f)
		if err != nil {
			if discovered[f] && errors.Is(err, junit.ErrNotJUnit) {
				res.Skipped = append(res.Skipped, f)
				continue
			}
			return nil, err
		}
		key := f
		if opt.GroupByDir {
			key = filepath.Dir(f)
		}
		run, ok := groups[key]
		if !ok {
			run = &Run{ID: filepath.ToSlash(key)}
			groups[key] = run
			order = append(order, key)
		}
		run.Files = append(run.Files, filepath.ToSlash(f))
		for _, s := range rep.Suites {
			if !s.Timestamp.IsZero() && (run.Time.IsZero() || s.Timestamp.Before(run.Time)) {
				run.Time = s.Timestamp
			}
			run.Cases = append(run.Cases, s.Cases...)
		}
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("no JUnit XML files found under %s", strings.Join(args, ", "))
	}

	for _, key := range order {
		res.Runs = append(res.Runs, *groups[key])
	}
	sort.SliceStable(res.Runs, func(i, j int) bool {
		a, b := res.Runs[i], res.Runs[j]
		if !a.Time.Equal(b.Time) {
			return a.Time.Before(b.Time)
		}
		return a.ID < b.ID
	})
	return res, nil
}

// expand resolves args into a sorted, deduplicated file list. The second
// return marks files that were discovered by walking (as opposed to being
// named explicitly), which are allowed to be non-JUnit.
func expand(args []string) ([]string, map[string]bool, error) {
	seen := make(map[string]bool)
	discovered := make(map[string]bool)
	var files []string
	add := func(path string, walked bool) {
		if !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
		if walked {
			discovered[path] = true
		}
	}
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, nil, err
		}
		if !info.IsDir() {
			add(filepath.Clean(arg), false)
			continue
		}
		err = filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".xml") {
				add(filepath.Clean(path), true)
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	sort.Strings(files)
	return files, discovered, nil
}
