package handler

import (
	"context"
	"fmt"
	"github.com/pennsieve/app-deploy-service/service/events"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pusher/pusher-http-go/v5"
	"log"
	"time"
)

// StatusManager is responsible for managing both Application and Deployment statuses. Any change to an Application or
// Deployment status should be routed through this object rather than directly to store_dynamodb, so that status updates are
// centralized, and we can update other interested parties here. For example, Pusher
type StatusManager struct {
	HandlerName       string
	ApplicationsStore store_dynamodb.DynamoDBStore
	DeploymentsStore  *store_dynamodb.DeploymentsStore
	Pusher            *pusher.Client
	ApplicationId     string
	DeploymentId      string
}

func NewStatusManager(handlerName string, applicationsStore store_dynamodb.DynamoDBStore, applicationId string) *StatusManager {
	return &StatusManager{HandlerName: handlerName, ApplicationsStore: applicationsStore, ApplicationId: applicationId}
}

func (m *StatusManager) WithDeployment(deploymentsStore *store_dynamodb.DeploymentsStore, deploymentId string) *StatusManager {
	m.DeploymentsStore = deploymentsStore
	m.DeploymentId = deploymentId
	return m
}

func (m *StatusManager) WithPusher(pusher *pusher.Client) *StatusManager {
	m.Pusher = pusher
	return m
}

func (m *StatusManager) SetErrorStatus(ctx context.Context, err error) string {
	msg := fmt.Sprintf("error: %s", err.Error())
	if appStoreErr := m.ApplicationsStore.UpdateStatus(ctx, msg, m.ApplicationId); appStoreErr != nil {
		log.Printf("warning: error updating applications table with error: %s: %s\n", msg, appStoreErr.Error())
	}
	if m.DeploymentsStore != nil {
		if deployStoreErr := m.DeploymentsStore.SetErrored(ctx, m.ApplicationId, m.DeploymentId); deployStoreErr != nil {
			log.Printf("warning: error setting errored on deployments table: %s\n", deployStoreErr.Error())
		}
	}
	m.sendApplicationStatusEvent(msg, true)
	return handlerError(m.HandlerName, err)
}

func (m *StatusManager) UpdateApplicationStatus(ctx context.Context, newStatus string, applicationUuid string) {
	if err := m.ApplicationsStore.UpdateStatus(ctx, newStatus, applicationUuid); err != nil {
		log.Printf("warning: error updating status of application %s to %q: %s\n", applicationUuid, newStatus, err.Error())
	}
	// In this module, this is never called for an error
	m.sendApplicationStatusEvent(newStatus, false)
}
func (m *StatusManager) NewApplication(ctx context.Context, application store_dynamodb.Application) error {
	m.sendApplicationStatusEvent(application.Status, false)
	return m.ApplicationsStore.Insert(ctx, application)
}

func (m *StatusManager) NewDeployment(ctx context.Context, deployment store_dynamodb.Deployment) error {
	if m.DeploymentsStore == nil {
		return fmt.Errorf("cannot create Deployment record: no DeploymentStore configured")
	}
	return m.DeploymentsStore.Insert(ctx, deployment)
}

func (m *StatusManager) sendApplicationStatusEvent(status string, isErrorStatus bool) {
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
