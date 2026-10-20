package snapshot

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func (repo repository) writeTree(ctx context.Context, dir string, entries map[string]entry) error {
	ordered := sortedEntries(entries)
	var input strings.Builder
	for _, file := range ordered {
		if err := validatePath(file.path); err != nil {
			return err
		}

		if file.mode != "100644" && file.mode != "100755" {
			return fmt.Errorf("%w: Git file mode %s at %q (symlink or submodule)", ErrUnsupportedLayout, file.mode, file.path)
		}

		input.WriteString(file.oid + "\n")
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	command := repo.command(ctx, "cat-file", "--batch")
	command.Stdin = strings.NewReader(input.String())
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("read Git objects: %w", err)
	}

	if err = command.Start(); err != nil {
		return fmt.Errorf("start Git object reader: %w", err)
	}

	err = copyObjects(bufio.NewReader(output), dir, ordered)
	if err != nil {
		_ = command.Process.Kill()
	}

	waitErr := command.Wait()
	if err != nil {
		return err
	}

	if waitErr != nil {
		return fmt.Errorf("read Git objects: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}

	return nil
}

func copyObjects(reader *bufio.Reader, dir string, entries []entry) error {
	for _, file := range entries {
		header, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read object header for %q: %w", file.path, err)
		}

		fields := strings.Fields(header)
		if len(fields) != 3 || fields[0] != file.oid || fields[1] != "blob" {
			return fmt.Errorf("%w: invalid blob header for %q", ErrInvalidGitOutput, file.path)
		}

		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 {
			return fmt.Errorf("%w: invalid blob size for %q", ErrInvalidGitOutput, file.path)
		}

		err = validatePath(file.path)
		if err != nil {
			return err
		}

		mode := os.FileMode(0o644)
		if file.mode == "100755" {
			mode = 0o755
		}

		if err = copyFile(dir, file.path, mode, io.LimitReader(reader, size)); err != nil {
			return err
		}

		if end, readErr := reader.ReadByte(); readErr != nil || end != '\n' {
			return fmt.Errorf("%w: invalid blob terminator", ErrInvalidGitOutput)
		}
	}

	return nil
}
