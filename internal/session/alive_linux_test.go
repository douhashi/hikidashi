package session

import (
	"os"
	"testing"

	"github.com/douhashi/hikidashi/internal/testutil"
)

func TestAliveOnlyForRunningClaude(t *testing.T) {
	for name, tc := range map[string]struct {
		pid  int
		want bool
	}{
		"running claude":        {testutil.StartClaude(t), true},
		"process of other name": {os.Getpid(), false},
		"exited process":        {testutil.DeadPID(t), false},
		"zero":                  {0, false},
		"negative":              {-1, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Alive(tc.pid)

			if err != nil || got != tc.want {
				t.Errorf("Alive(%d) = %v, err %v, want %v, nil", tc.pid, got, err, tc.want)
			}
		})
	}
}
