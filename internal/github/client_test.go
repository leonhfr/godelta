package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/leonhfr/godelta/internal/github"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	for _, selector := range []string{"", "code.example/owner/project"} {
		t.Run(selector, func(t *testing.T) {
			t.Parallel()

			base, head, ancestor := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
			commands := [][]string{}
			responses := []string{
				`{"nameWithOwner":"owner/project","url":"https://code.example/owner/project"}`,
				`{"number":123,"url":"https://code.example/owner/project/pull/123","baseRefOid":"` + base + `","headRefOid":"` + head + `"}`,
				`{"merge_base_commit":{"sha":"` + ancestor + `"}}`,
			}
			client := github.Client{Command: func(_ context.Context, dir string, args ...string) ([]byte, error) {
				assert.Equal(t, "checkout", dir)

				commands = append(commands, args)
				return []byte(responses[len(commands)-1]), nil
			}}
			pr, err := client.Resolve(t.Context(), "checkout", selector, 123)
			require.NoError(t, err)
			assert.Equal(t, "https://code.example/owner/project/pull/123", pr.URL)
			assert.Equal(t, "https://code.example/owner/project.git", pr.Remote.URL)
			assert.Equal(t, ancestor, pr.Remote.BaseCommit)
			assert.Equal(t, head, pr.Remote.HeadCommit)
			assert.Equal(t, "refs/pull/123/head", pr.Remote.HeadRef)

			repoArgs := []string{"repo", "view", "--json", "nameWithOwner,url"}
			if selector != "" {
				repoArgs = append(repoArgs, "--", selector)
			}

			assert.Equal(
				t,
				[][]string{
					repoArgs,
					{"pr", "view", "123", "--repo", "code.example/owner/project", "--json", "number,url,baseRefOid,headRefOid"},
					{"api", "--hostname", "code.example", "--method", "GET", "repos/owner/project/compare/" + base + "..." + head + "?per_page=1"},
				},
				commands,
			)
		})
	}
}

func TestResolveFailures(t *testing.T) {
	t.Parallel()

	sha := strings.Repeat("a", 40)
	validRepo := `{"nameWithOwner":"owner/project","url":"https://github.com/owner/project"}`
	validPR := `{"number":1,"url":"https://github.com/owner/project/pull/1","baseRefOid":"` + sha + `","headRefOid":"` + sha + `"}`
	cases := []struct {
		name      string
		responses []string
	}{
		{"invalid JSON", []string{"{"}},
		{"missing repository", []string{`{}`}},
		{"unsafe URL", []string{`{"nameWithOwner":"owner/project","url":"https://user:password@github.com/owner/project"}`}},
		{"wrong path", []string{`{"nameWithOwner":"owner/project","url":"https://github.com/other/project"}`}},
		{"missing PR", []string{validRepo, `{}`}},
		{"missing ancestor", []string{validRepo, validPR, `{}`}},
		{"malformed ancestor", []string{validRepo, validPR, `{"merge_base_commit":{"sha":"--help"}}`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			index := 0
			client := github.Client{Command: func(context.Context, string, ...string) ([]byte, error) {
				require.Less(t, index, len(test.responses))

				result := test.responses[index]
				index++
				return []byte(result), nil
			}}
			_, err := client.Resolve(t.Context(), "", "", 1)
			require.ErrorIs(t, err, github.ErrInvalidResponse)

			if test.name == "invalid JSON" {
				var syntax *json.SyntaxError
				require.ErrorAs(t, err, &syntax)
			}
		})
	}

	for _, step := range []int{0, 1, 2} {
		t.Run(string(rune('0'+step)), func(t *testing.T) {
			t.Parallel()

			cause := errors.New("lookup failed")
			responses := []string{validRepo, validPR}
			index := 0
			client := github.Client{Command: func(context.Context, string, ...string) ([]byte, error) {
				if index == step {
					return nil, cause
				}

				response := responses[index]
				index++
				return []byte(response), nil
			}}
			_, err := client.Resolve(t.Context(), "", "", 1)
			require.ErrorIs(t, err, github.ErrLookup)
			require.ErrorIs(t, err, cause)
		})
	}

	_, err := (github.Client{}).Resolve(t.Context(), "", "", 0)
	require.ErrorIs(t, err, github.ErrInvalidRequest)
}
