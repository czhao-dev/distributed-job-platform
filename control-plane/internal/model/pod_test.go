package model

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTransitionPod(t *testing.T) {
	allStatuses := []PodStatus{
		PodPending, PodScheduled, PodRunning, PodSucceeded,
		PodFailed, PodRetrying, PodDeadLetter, PodCancelled,
	}

	valid := map[PodStatus]map[PodStatus]bool{
		PodPending:    {PodScheduled: true, PodCancelled: true},
		PodScheduled:  {PodRunning: true, PodPending: true, PodCancelled: true},
		PodRunning:    {PodSucceeded: true, PodFailed: true, PodCancelled: true},
		PodFailed:     {PodRetrying: true, PodDeadLetter: true},
		PodRetrying:   {PodPending: true, PodScheduled: true},
		PodSucceeded:  {},
		PodDeadLetter: {},
		PodCancelled:  {},
	}

	for _, from := range allStatuses {
		for _, to := range allStatuses {
			from, to := from, to
			want := valid[from][to]
			t.Run(fmt.Sprintf("%s_to_%s", from, to), func(t *testing.T) {
				assert.Equal(t, want, TransitionPod(from, to))
			})
		}
	}
}

func TestPodClone(t *testing.T) {
	exitCode := 0
	pod := Pod{ID: "pod_1", Args: []string{"a", "b"}, ExitCode: &exitCode, Labels: map[string]string{"app": "test"}}
	clone := pod.Clone()

	clone.Args[0] = "mutated"
	*clone.ExitCode = 99
	clone.Labels["app"] = "mutated"

	assert.Equal(t, "a", pod.Args[0], "mutating a clone must not affect the original")
	assert.Equal(t, 0, *pod.ExitCode, "mutating a clone's ExitCode must not affect the original")
	assert.Equal(t, "test", pod.Labels["app"], "mutating clone Labels must not affect original")
}

func TestPodActive(t *testing.T) {
	cases := []struct {
		status PodStatus
		active bool
	}{
		{PodPending, true},
		{PodScheduled, true},
		{PodRunning, true},
		{PodRetrying, true},
		{PodFailed, true},
		{PodSucceeded, true},
		{PodDeadLetter, false},
		{PodCancelled, false},
	}
	for _, c := range cases {
		pod := Pod{Status: c.status}
		assert.Equal(t, c.active, pod.Active(), "status %s", c.status)
	}
}
