package snapshot

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/leonhfr/godelta/internal/project"
)

type entry struct {
	path string
	mode string
	oid  string
}

func (repo repository) tree(ctx context.Context, commit string) (map[string]entry, error) {
	output, err := repo.run(ctx, "ls-tree", "-rz", "--full-tree", commit)
	if err != nil {
		return nil, err
	}

	entries := make(map[string]entry)
	for record := range strings.SplitSeq(strings.TrimSuffix(string(output), "\x00"), "\x00") {
		if record == "" {
			continue
		}

		metadata, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(metadata)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("%w: invalid tree entry", ErrInvalidGitOutput)
		}

		entries[name] = entry{path: name, mode: fields[0], oid: fields[2]}
	}

	return entries, nil
}

func (repo repository) stagedTree(ctx context.Context, head map[string]entry, commit string) (map[string]entry, error) {
	output, err := repo.run(
		ctx,
		"diff",
		"--cached",
		"--raw",
		"-z",
		"--no-renames",
		"--no-abbrev",
		"--no-ext-diff",
		"--no-textconv",
		"--ita-invisible-in-index",
		commit,
		"--",
	)
	if err != nil {
		return nil, err
	}

	entries := maps.Clone(head)
	records := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
	for index := 0; index < len(records) && records[index] != ""; index += 2 {
		fields := strings.Fields(records[index])
		if len(fields) != 5 || index+1 >= len(records) {
			return nil, fmt.Errorf("%w: invalid index diff entry", ErrInvalidGitOutput)
		}

		name := records[index+1]
		if fields[4] == "D" {
			delete(entries, name)
		} else {
			entries[name] = entry{path: name, mode: fields[1], oid: fields[3]}
		}
	}

	return entries, nil
}

func (repo repository) materialize(ctx context.Context, comparison *Comparison) error {
	before, err := repo.tree(ctx, comparison.BaseCommit)
	if err != nil {
		return err
	}

	if err = repo.writeTree(ctx, comparison.Before, before); err != nil {
		return fmt.Errorf("prepare old snapshot: %w", err)
	}

	switch comparison.Mode {
	case WorkingTree:
		err = repo.writeWorkingTree(ctx, comparison.After)

	case Staged:
		var staged map[string]entry
		staged, err = repo.stagedTree(ctx, before, comparison.HeadCommit)
		if err == nil {
			err = repo.writeTree(ctx, comparison.After, staged)
		}

	case Committed:
		var committed map[string]entry
		committed, err = repo.tree(ctx, comparison.HeadCommit)
		if err == nil {
			err = repo.writeTree(ctx, comparison.After, committed)
		}
	}

	if err != nil {
		return fmt.Errorf("prepare new snapshot: %w", err)
	}

	for _, dir := range []string{comparison.Before, comparison.After} {
		if _, err = project.Discover(dir); err != nil {
			return err
		}
	}

	return nil
}

func sortedEntries(entries map[string]entry) []entry {
	paths := slices.Sorted(maps.Keys(entries))
	result := make([]entry, 0, len(paths))
	for _, path := range paths {
		result = append(result, entries[path])
	}

	return result
}
