#!/bin/sh

echo "RUNNING IN ENVIRONMENT: $ENV"

TERRAFORM_DIR="/usr/src/app/terraform/pennsieve/public-repository"
cd $TERRAFORM_DIR
VAR_FILE="$TERRAFORM_DIR/public_repository.tfvars"
OUTPUT_FILE="$TERRAFORM_DIR/outputs.json"

echo "Running init and plan ..."

echo "Creating tfvars config"
  /bin/cat > $VAR_FILE <<EOL
region = "$AWS_DEFAULT_REGION"
env = "$ENV"
source_url = "$1"
EOL

echo "Running init and plan ..."
export TF_LOG_PATH="error.log"
export TF_LOG=TRACE
terraform init
terraform plan -out=tfplan -var-file=$VAR_FILE

echo "Running apply ..."
terraform apply tfplan
terraform output -json > $OUTPUT_FILE

cat error.log
echo "DONE RUNNING IN ENVIRONMENT: $ENV"