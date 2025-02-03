package status

import (
	"context"
	"fmt"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/status/events"
	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/store_dynamodb"
	pennsievePusher "github.com/pennsieve/pennsieve-go-core/pkg/models/pusher"
	"github.com/pusher/pusher-http-go/v5"
	"log"
	"time"
)

// Manager is responsible for managing both Application and Deployment statuses. Any change to an Application or
// Deployment status should be routed through this object rather than directly to store_dynamodb, so that status updates are
// centralized, and we can update other interested parties here. For example, Pusher
type Manager struct {
	HandlerName       string
	ApplicationsStore store_dynamodb.DynamoDBStore
	DeploymentsStore  *store_dynamodb.DeploymentsStore
	Pusher            *pusher.Client
	ApplicationId     string
	DeploymentId      string
}

func NewManager(applicationsStore store_dynamodb.DynamoDBStore, applicationId string) *Manager {
	return &Manager{HandlerName: "AppProvisioner", ApplicationsStore: applicationsStore, ApplicationId: applicationId}
}

func (m *Manager) WithDeployment(deploymentsStore *store_dynamodb.DeploymentsStore, deploymentId string) *Manager {
	m.DeploymentsStore = deploymentsStore
	m.DeploymentId = deploymentId
	return m
}

func (m *Manager) WithPusher(pusherConfig *pennsievePusher.Config) *Manager {
	m.Pusher = &pusher.Client{
		AppID:   pusherConfig.AppId,
		Key:     pusherConfig.Key,
		Secret:  pusherConfig.Secret,
		Cluster: pusherConfig.Cluster,
		Secure:  true,
	}
	return m
}

func (m *Manager) SetErrorStatus(ctx context.Context, err error) {
	msg := fmt.Sprintf("error: %s", err.Error())
	if appStoreErr := m.ApplicationsStore.UpdateStatus(ctx, msg, m.ApplicationId); appStoreErr != nil {
		log.Printf("warning: error updating applications table with error: %s: %s\n", msg, appStoreErr.Error())
	}
	if m.DeploymentsStore != nil {
		if deployStoreErr := m.DeploymentsStore.SetErroredFlag(ctx, m.ApplicationId, m.DeploymentId); deployStoreErr != nil {
			log.Printf("warning: error setting errored on deployments table: %s\n", deployStoreErr.Error())
		}
	}
	m.sendApplicationStatusEvent(msg, true)
}

func (m *Manager) UpdateApplicationStatus(ctx context.Context, newStatus string, isError bool) {
	if err := m.ApplicationsStore.UpdateStatus(ctx, newStatus, m.ApplicationId); err != nil {
		log.Printf("warning: error updating status of application %s to %q: %s\n", m.ApplicationId, newStatus, err.Error())
	}
	m.sendApplicationStatusEvent(newStatus, isError)
}

func (m *Manager) ApplicationCreateUpdate(ctx context.Context, application store_dynamodb.Application) error {
	status := application.Status
	m.sendApplicationStatusEvent(status, false)
	return m.ApplicationsStore.Update(ctx, application, m.ApplicationId)
}

func (m *Manager) sendApplicationStatusEvent(status string, isErrorStatus bool) {
	if m.Pusher == nil {
		log.Printf("warning: no Pusher client configured")
		return
	}
	channel := events.ApplicationStatusChannel(m.ApplicationId)
	event := events.ApplicationStatusEvent{
		ApplicationId: m.ApplicationId,
		DeploymentId:  m.DeploymentId,
		Status:        status,
		Time:          time.Now().UTC(),
		IsErrorStatus: isErrorStatus,
		Source:        m.HandlerName,
	}
	if err := m.Pusher.Trigger(channel, events.ApplicationStatusEventName, event); err != nil {
		log.Printf("warning: error updating pusher application channel %s with status: %s: %s\n", channel, status, err.Error())
	}
}
