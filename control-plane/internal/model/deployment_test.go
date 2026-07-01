package model

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTransitionDeployment(t *testing.T) {
	allStatuses := []DeploymentStatus{DeploymentPending, DeploymentActive, DeploymentDegraded, DeploymentCancelled}

	valid := map[DeploymentStatus]map[DeploymentStatus]bool{
		DeploymentPending:   {DeploymentActive: true, DeploymentCancelled: true},
		DeploymentActive:    {DeploymentDegraded: true, DeploymentCancelled: true},
		DeploymentDegraded:  {DeploymentActive: true, DeploymentCancelled: true},
		DeploymentCancelled: {},
	}

	for _, from := range allStatuses {
		for _, to := range allStatuses {
			from, to := from, to
			want := valid[from][to]
			t.Run(fmt.Sprintf("%s_to_%s", from, to), func(t *testing.T) {
				assert.Equal(t, want, TransitionDeployment(from, to))
			})
		}
	}
}

func TestDeploymentClone(t *testing.T) {
	d := Deployment{ID: "d1", Args: []string{"a"}, Labels: map[string]string{"app": "test"}}
	clone := d.Clone()
	clone.Args[0] = "mutated"
	clone.Labels["app"] = "mutated"
	assert.Equal(t, "a", d.Args[0], "mutating clone Args must not affect original")
	assert.Equal(t, "test", d.Labels["app"], "mutating clone Labels must not affect original")
}
