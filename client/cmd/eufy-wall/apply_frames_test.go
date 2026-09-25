package main

import "testing"

func TestLaunchdPID(t *testing.T) {
	if got, err := launchdPID("state = running\n\tpid = 9606\njob state = running\n"); err != nil || got != 9606 {
		t.Fatalf("PID = %d, %v", got, err)
	}
	for _, report := range []string{"pid = 0", "pid = nope", "state = waiting"} {
		if _, err := launchdPID(report); err == nil {
			t.Fatalf("accepted %q", report)
		}
	}
}
