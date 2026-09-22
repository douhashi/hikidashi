package main

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

// usageHead は使い方の文面の書き出し。
const usageHead = "Usage: hikidashi <command>"

// invoke は stdin を与えて run を呼び、終了コードと stdout・stderr の中身を返す。
func invoke(cmds []command, stdin string, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(cmds, args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRunWithoutArgsPrintsUsageToStderr(t *testing.T) {
	code, stdout, stderr := invoke(nil, "")

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.HasPrefix(stderr, usageHead) {
		t.Errorf("stderr = %q, want usage", stderr)
	}
}

func TestRunHelpPrintsUsageToStdout(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			code, stdout, stderr := invoke(nil, "", arg)

			if code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}
			if !strings.HasPrefix(stdout, usageHead) {
				t.Errorf("stdout = %q, want usage", stdout)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

// failingWriter は書き込みを常に失敗させる。閉じたパイプ等への出力を模す。
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestRunHelpFailsWhenStdoutIsNotWritable(t *testing.T) {
	var errOut bytes.Buffer

	code := run(nil, []string{"help"}, strings.NewReader(""), failingWriter{}, &errOut)

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "hikidashi: broken pipe\n"; errOut.String() != want {
		t.Errorf("stderr = %q, want %q", errOut.String(), want)
	}
}

func TestRunUnknownCommandReportsErrorAndUsage(t *testing.T) {
	code, stdout, stderr := invoke(nil, "", "nope")

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.HasPrefix(stderr, `hikidashi: unknown command "nope"`+"\n") {
		t.Errorf("stderr = %q, want unknown command error first", stderr)
	}
	if !strings.Contains(stderr, usageHead) {
		t.Errorf("stderr = %q, want usage", stderr)
	}
}

func TestUsageListsCommands(t *testing.T) {
	cmds := []command{
		{name: "hook", summary: "record the session state"},
		{name: "status", summary: "print the number of waiting sessions"},
	}

	_, stdout, _ := invoke(cmds, "", "help")

	for _, want := range []string{
		"hook    record the session state",
		"status  print the number of waiting sessions",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want line %q", stdout, want)
		}
	}
}

func TestUsageOmitsCommandSectionWhenTableIsEmpty(t *testing.T) {
	_, stdout, _ := invoke(nil, "", "help")

	if strings.Contains(stdout, "Commands:") {
		t.Errorf("stdout = %q, want no command section", stdout)
	}
}

func TestRunDispatchesToCommand(t *testing.T) {
	var gotArgs []string
	var gotInput []byte
	cmds := []command{
		{name: "other", run: func([]string, io.Reader, io.Writer, io.Writer) int {
			t.Error("other command must not run")
			return 1
		}},
		{name: "status", run: func(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
			gotArgs = args
			gotInput, _ = io.ReadAll(stdin)
			_, _ = io.WriteString(stdout, "out")
			_, _ = io.WriteString(stderr, "err")
			return 3
		}},
	}

	code, stdout, stderr := invoke(cmds, "in", "status", "-x", "y")

	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
	if want := []string{"-x", "y"}; !slices.Equal(gotArgs, want) {
		t.Errorf("args = %q, want %q", gotArgs, want)
	}
	if string(gotInput) != "in" {
		t.Errorf("stdin = %q, want %q", gotInput, "in")
	}
	if stdout != "out" || stderr != "err" {
		t.Errorf("stdout, stderr = %q, %q, want %q, %q", stdout, stderr, "out", "err")
	}
}
