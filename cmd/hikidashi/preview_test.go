package main

import (
	"strings"
	"testing"
)

func TestPreviewDrawsGivenIssuesWithoutGh(t *testing.T) {
	for issues, want := range map[string]string{"7": "7 open", "0": "0 open", "?": "?"} {
		t.Run(issues, func(t *testing.T) {
			env := newShowEnv(t)
			api := env.drawer(t, "api", "api-0123abcd")
			env.gh.OpenIssues(t, api.Path, 3)

			code, stdout, stderr := invoke(commands, "", "__preview", api.Slug(), issues)

			// ? は一覧で件数が得られなかった印で、理由は一覧の側で捨てているため stderr にも出さない。
			if code != 0 || stderr != "" {
				t.Errorf("__preview = %d, stderr %q, want 0 and silent", code, stderr)
			}
			if wantFrame := drawerFrame(api.Path, want); !strings.HasPrefix(stdout, wantFrame) {
				t.Errorf("stdout =\n%s\nwant to start with\n%s", stdout, wantFrame)
			}
			if got := env.gh.Calls(t); got != nil {
				t.Errorf("gh ran with %+v, want not run", got)
			}
		})
	}
}

func TestPreviewDrawsLikeShowForTheSameIssues(t *testing.T) {
	api := longDetailEnv(t)
	t.Setenv("FZF_PREVIEW_COLUMNS", "140")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	_, want, _ := invoke(commands, "", "show", api.Slug())

	// 幅と色は hikidashi show と同じ規則で決める。
	code, stdout, _ := invoke(commands, "", "__preview", api.Slug(), "1")

	if code != 0 || stdout != want {
		t.Errorf("__preview = %d, stdout =\n%s\nwant the same as show\n%s", code, stdout, want)
	}
}

func TestPreviewFailsForUnknownDrawer(t *testing.T) {
	newShowEnv(t)

	code, stdout, stderr := invoke(commands, "", "__preview", "nope", "1")

	if want := "hikidashi __preview: no drawer \"nope\"\n"; code != 1 || stdout != "" || stderr != want {
		t.Errorf("__preview = %d, stdout %q, stderr %q, want 1 and %q", code, stdout, stderr, want)
	}
}

func TestPreviewRejectsBadArguments(t *testing.T) {
	for name, args := range map[string][]string{
		"none":         nil,
		"slug only":    {"api"},
		"extra":        {"api", "1", "x"},
		"not a number": {"api", "many"},
		"negative":     {"api", "-1"},
		"empty":        {"api", ""},
	} {
		t.Run(name, func(t *testing.T) {
			env := newShowEnv(t)
			env.drawer(t, "api", "api-0123abcd")

			code, stdout, stderr := invoke(commands, "", append([]string{"__preview"}, args...)...)

			if want := "Usage: hikidashi __preview <drawer> <issues|?>\n"; code != 2 || stdout != "" || stderr != want {
				t.Errorf("__preview %q = %d, stdout %q, stderr %q, want 2 and %q", args, code, stdout, stderr, want)
			}
		})
	}
}
