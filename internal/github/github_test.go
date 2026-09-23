package github

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/testutil"
)

func TestOpenIssuesRunsGhAtRepositoryRoot(t *testing.T) {
	gh := testutil.NewFakeGh(t)
	repo := t.TempDir()
	gh.OpenIssues(t, repo, 7)

	got, err := OpenIssues(context.Background(), repo)

	if err != nil || got != 7 {
		t.Errorf("OpenIssues = %d, err %v, want 7", got, err)
	}
	calls := gh.Calls(t)
	want, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Dir != want || !slices.Equal(calls[0].Args, []string{"repo", "view", "--json", "issues"}) {
		t.Errorf("gh calls = %+v, want one repo view --json issues at %s", calls, want)
	}
}

func TestOpenIssuesIgnoresForcedColor(t *testing.T) {
	gh := testutil.NewFakeGh(t)
	repo := t.TempDir()
	gh.OpenIssues(t, repo, 7)
	// hikidashi open の fzf のプレビューは、色を出させるため CLICOLOR_FORCE=1 で hikidashi show を起動する。
	t.Setenv("CLICOLOR_FORCE", "1")

	if got, err := OpenIssues(context.Background(), repo); err != nil || got != 7 {
		t.Errorf("OpenIssues = %d, err %v, want 7", got, err)
	}
}

func TestOpenIssuesZeroIsACount(t *testing.T) {
	gh := testutil.NewFakeGh(t)
	repo := t.TempDir()
	gh.OpenIssues(t, repo, 0)

	if got, err := OpenIssues(context.Background(), repo); err != nil || got != 0 {
		t.Errorf("OpenIssues = %d, err %v, want 0", got, err)
	}
}

func TestOpenIssuesFailsWithReason(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(t *testing.T, gh *testutil.FakeGh, repo string)
		want  string
	}{
		"no GitHub remote": {
			setup: func(*testing.T, *testutil.FakeGh, string) {},
			want:  "gh repo view --json issues: exit status 1: none of the git remotes configured for this repository point to a known GitHub host",
		},
		"not authenticated": {
			setup: func(t *testing.T, gh *testutil.FakeGh, repo string) {
				gh.Fail(t, repo, "To get started with GitHub CLI, please run:  gh auth login", 4)
			},
			want: "gh repo view --json issues: exit status 4: To get started with GitHub CLI, please run:  gh auth login",
		},
		"unexpected output": {
			setup: func(t *testing.T, gh *testutil.FakeGh, repo string) { gh.Respond(t, repo, `{"issues":{}}`, 0) },
			want:  `gh repo view --json issues: unexpected output "{\"issues\":{}}"`,
		},
		"not JSON": {
			setup: func(t *testing.T, gh *testutil.FakeGh, repo string) { gh.Respond(t, repo, "oops", 0) },
			want:  "gh repo view --json issues: decode output: ",
		},
	} {
		t.Run(name, func(t *testing.T) {
			gh := testutil.NewFakeGh(t)
			repo := t.TempDir()
			tc.setup(t, gh, repo)

			got, err := OpenIssues(context.Background(), repo)

			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Errorf("OpenIssues = %d, err %v, want error %q", got, err, tc.want)
			}
		})
	}
}

func TestOpenIssuesFailsWithoutGh(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if _, err := OpenIssues(context.Background(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "executable file not found") {
		t.Errorf("OpenIssues err = %v, want gh not found", err)
	}
}

func TestOpenIssuesStopsGhAtDeadline(t *testing.T) {
	gh := testutil.NewFakeGh(t)
	repo := t.TempDir()
	gh.Hang(t, repo)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()

	_, err := OpenIssues(ctx, repo)

	if want := "gh repo view --json issues: context deadline exceeded"; err == nil || err.Error() != want {
		t.Errorf("OpenIssues err = %v, want %q", err, want)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("OpenIssues took %v, want it stopped at the deadline", elapsed)
	}
}
