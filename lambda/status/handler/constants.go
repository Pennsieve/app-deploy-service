package handler

const ApplicationsTableEnvVar = "APPLICATIONS_TABLE"
const DeploymentsTableEnvVar = "DEPLOYMENTS_TABLE"

// DeploymentIdTag is the tag that we add to the deployment ECS task so that the deployment id can be retrieved by
// the state change listener
const DeploymentIdTag = "DeploymentId"
