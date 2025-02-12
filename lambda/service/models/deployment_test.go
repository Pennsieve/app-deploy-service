package models

import (
	"github.com/stretchr/testify/assert"
	"slices"
	"testing"
	"time"
)

func TestDeploymentsByInitiatedAtAsc(t *testing.T) {
	beforeInitiatedAt := time.Now().UTC()
	afterInitiatedAt := beforeInitiatedAt.Add(1 * time.Hour)
	before := Deployment{
		DeploymentId:  "before",
		ApplicationId: "app-1",
		InitiatedAt:   beforeInitiatedAt,
	}
	after := Deployment{
		DeploymentId:  "after",
		ApplicationId: "app-1",
		InitiatedAt:   afterInitiatedAt,
	}

	deployments := Deployments{Deployments: []Deployment{after, before}}

	slices.SortFunc(deployments.Deployments, DeploymentsByInitiatedAtAsc)

	assert.Equal(t, before.DeploymentId, deployments.Deployments[0].DeploymentId)
	assert.Equal(t, after.DeploymentId, deployments.Deployments[1].DeploymentId)
}
