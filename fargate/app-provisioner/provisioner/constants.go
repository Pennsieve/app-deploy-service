package provisioner

// DeploymentIdKey is the env variable holding the deployment id
const DeploymentIdKey = "DEPLOYMENT_ID"

// DeploymentsTableNameKey is the env var holding the name of the deployments table
const DeploymentsTableNameKey = "DEPLOYMENTS_TABLE"

// DeploymentIdTag is the tag that we add to the deployment ECS task so that the deployment id can be retrieved by
// the state change listener
const DeploymentIdTag = "DeploymentId"

// ApplicationIdTag is the tag that we add to the deployment ECS task so that the application id can be retrieved by
// the state change listener
const ApplicationIdTag = "ApplicationId"
