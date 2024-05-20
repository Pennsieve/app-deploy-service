#!/bin/sh

echo "RUNNING IN ENVIRONMENT: $ENV"

TERRAFORM_DIR="/usr/src/app/terraform/infrastructure"
cd $TERRAFORM_DIR
VAR_FILE="$TERRAFORM_DIR/application.tfvars"
BACKEND_FILE="$TERRAFORM_DIR/application.tfbackend"

export AWS_ACCESS_KEY_ID=$2
export AWS_SECRET_ACCESS_KEY=$3
export AWS_SESSION_TOKEN=$4

echo "Creating backend config"
  /bin/cat > $BACKEND_FILE <<EOL
bucket  = "tfstate-$1"
key     = "$ENV/apps/$GIT_URL/terraform.tfstate"
EOL

echo "Creating tfvars config"
  /bin/cat > $VAR_FILE <<EOL
account_id = "$1"
region = "${AWS_DEFAULT_REGION}"
env = "$ENV"
wm_cpu = "${WM_CPU:-2048}"
wm_memory = "${WM_MEMORY:-4096}"
az = ["a", "b", "c", "d", "e", "f"]
EOL


echo "Running init and destroy ..."
export TF_LOG_PATH="error.log"
export TF_LOG=TRACE
terraform init -force-copy -backend-config=$BACKEND_FILE

echo "Running destroy ..."
terraform apply -destroy -auto-approve -var-file=$VAR_FILE

echo "DONE RUNNING IN ENVIRONMENT: $ENV"