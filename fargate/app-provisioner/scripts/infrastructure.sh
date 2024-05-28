#!/bin/sh

echo "RUNNING IN ENVIRONMENT: $ENV"

TERRAFORM_DIR="/usr/src/app/terraform/infrastructure"
cd $TERRAFORM_DIR
VAR_FILE="$TERRAFORM_DIR/application.tfvars"
BACKEND_FILE="$TERRAFORM_DIR/application.tfbackend"
OUTPUT_FILE="$TERRAFORM_DIR/outputs.json"

export AWS_ACCESS_KEY_ID=$2
export AWS_SECRET_ACCESS_KEY=$3
export AWS_SESSION_TOKEN=$4

echo "Creating backend config"
  /bin/cat > $BACKEND_FILE <<EOL
bucket  = "tfstate-$1"
key     = "$ENV/apps/$5/terraform.tfstate"
EOL

echo "cpu: ${APP_CPU}"
echo "memory: ${APP_MEMORY}"
echo "Running init and plan ..."

cat $BACKEND_FILE

echo "Creating tfvars config"
  /bin/cat > $VAR_FILE <<EOL
account_id = "$1"
region = "$AWS_DEFAULT_REGION"
env = "$ENV"
app_cpu = "${APP_CPU:-2048}"
app_memory = "${APP_MEMORY:-4096}"
compute_node_efs_id = "$6"
app_slug = "$7"
EOL

cat $VAR_FILE

echo "Running init and plan ..."
export TF_LOG_PATH="error.log"
export TF_LOG=TRACE
terraform init -force-copy -backend-config=$BACKEND_FILE
terraform plan -out=tfplan -var-file=$VAR_FILE

echo "Running apply ..."
terraform apply tfplan
terraform output -json > $OUTPUT_FILE

pwd
cat error.log | grep "[ERROR"

echo "DONE RUNNING IN ENVIRONMENT: $ENV"