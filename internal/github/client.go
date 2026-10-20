// Package github resolves pull requests through the authenticated GitHub CLI.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/leonhfr/godelta/internal/snapshot"
)

var (
	// ErrInvalidRequest indicates an invalid PR selection.
	ErrInvalidRequest = errors.New("invalid pull request selection")
	// ErrLookup indicates a failed GitHub CLI operation.
	ErrLookup = errors.New("GitHub lookup failed")
	// ErrInvalidResponse indicates unusable GitHub metadata.
	ErrInvalidResponse = errors.New("invalid GitHub response")
	repoPattern        = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	shaPattern         = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Command runs gh in dir. A nil Command uses the installed executable.
type Command func(ctx context.Context, dir string, args ...string) ([]byte, error)

// Client uses gh authentication without writing authentication configuration.
type Client struct{ Command Command }

// PullRequest contains the review URL and remote snapshot inputs.
type PullRequest struct {
	URL    string
	Remote snapshot.Remote
}

// Resolve reads repository, PR, and merge-base metadata, including fork heads.
func (client Client) Resolve(ctx context.Context, dir, selector string, number int) (PullRequest, error) {
	if number <= 0 {
		return PullRequest{}, ErrInvalidRequest
	}

	args := []string{"repo", "view", "--json", "nameWithOwner,url"}
	if selector != "" {
		args = append(args, "--", selector)
	}

	var repo struct {
		NameWithOwner string
		URL           string
	}
	if err := client.read(ctx, dir, &repo, args...); err != nil {
		return PullRequest{}, err
	}

	host, err := repositoryHost(repo.URL, repo.NameWithOwner)
	if err != nil {
		return PullRequest{}, err
	}

	var pr struct {
		Number                      int
		URL, BaseRefOid, HeadRefOid string
	}
	if err = client.read(
		ctx,
		dir,
		&pr,
		"pr",
		"view",
		strconv.Itoa(number),
		"--repo",
		host+"/"+repo.NameWithOwner,
		"--json",
		"number,url,baseRefOid,headRefOid",
	); err != nil {
		return PullRequest{}, err
	}

	expectedURL := repo.URL + "/pull/" + strconv.Itoa(number)
	if pr.Number != number || pr.URL != expectedURL || !shaPattern.MatchString(pr.BaseRefOid) || !shaPattern.MatchString(pr.HeadRefOid) {
		return PullRequest{}, fmt.Errorf("%w: invalid PR revisions or URL", ErrInvalidResponse)
	}

	var comparison struct {
		MergeBaseCommit struct{ SHA string } `json:"merge_base_commit"`
	}
	if err = client.read(
		ctx,
		dir,
		&comparison,
		"api",
		"--hostname",
		host,
		"--method",
		"GET",
		"repos/"+repo.NameWithOwner+"/compare/"+pr.BaseRefOid+"..."+pr.HeadRefOid+"?per_page=1",
	); err != nil {
		return PullRequest{}, err
	}

	if !shaPattern.MatchString(comparison.MergeBaseCommit.SHA) {
		return PullRequest{}, fmt.Errorf("%w: missing merge base", ErrInvalidResponse)
	}

	return PullRequest{URL: pr.URL, Remote: snapshot.Remote{
		URL:        repo.URL + ".git",
		BaseCommit: comparison.MergeBaseCommit.SHA,
		HeadCommit: pr.HeadRefOid,
		HeadRef:    "refs/pull/" + strconv.Itoa(number) + "/head",
	}}, nil
}

func (client Client) read(ctx context.Context, dir string, target any, args ...string) error {
	run := client.Command
	if run == nil {
		run = runGH
	}

	output, err := run(ctx, dir, args...)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrLookup, err)
	}

	if err = json.Unmarshal(output, target); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidResponse, err)
	}

	return nil
}

func runGH(ctx context.Context, dir string, args ...string) ([]byte, error) {
	//nolint:gosec // Fixed executable and separate arguments; no shell evaluation.
	command := exec.CommandContext(ctx, "gh", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}

	return output, nil
}

func repositoryHost(address, name string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || !repoPattern.MatchString(name) || parsed.Path != "/"+name {
		return "", fmt.Errorf("%w: invalid repository URL", ErrInvalidResponse)
	}

	return parsed.Host, nil
}
