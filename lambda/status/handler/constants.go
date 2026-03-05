package handler

const ApplicationsTableEnvVar = "APPLICATIONS_TABLE"
const DeploymentsTableEnvVar = "DEPLOYMENTS_TABLE"

// DeploymentIdTag is the tag that we add to the deployment ECS task so that the deployment id can be retrieved by
// the state change listener
const DeploymentIdTag = "DeploymentId"

// ApplicationIdTag is the tag that we add to the deployment ECS task so that the application id can be retrieved by
// the state change listener
const ApplicationIdTag = "ApplicationId"

// ApplicationsTableTag is the tag that overrides the default applications table for the deployment.
// Used by appstore deployments to point the status handler at the appstore-specific table.
const ApplicationsTableTag = "ApplicationsTable"
