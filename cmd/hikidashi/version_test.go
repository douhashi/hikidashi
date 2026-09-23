package main

import (
	"runtime/debug"
	"testing"
)

func TestVersionPrintsTheVersionOfTheBuild(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("build info is not available in the test binary")
	}

	code, stdout, stderr := invoke(commands, "", "version")

	if code != 0 {
		t.Errorf("exit code = %d, want 0 (stderr = %q)", code, stderr)
	}
	if want := info.Main.Version + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestVersionRejectsArguments(t *testing.T) {
	code, stdout, stderr := invoke(commands, "", "version", "extra")

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if want := versionUsage; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}
