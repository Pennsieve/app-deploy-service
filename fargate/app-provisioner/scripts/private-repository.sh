#!/bin/sh

echo "RUNNING IN ENVIRONMENT: $ENV"

TERRAFORM_DIR="/usr/src/app/terraform/pennsieve/private-repository"
cd $TERRAFORM_DIR
VAR_FILE="$TERRAFORM_DIR/private_repository.tfvars"
OUTPUT_FILE="$TERRAFORM_DIR/outputs.json"

echo "Creating tfvars config"
/bin/cat > $VAR_FILE <<EOL
region = "$AWS_DEFAULT_REGION"
env = "$ENV"
aws_account = "$AWS_ACCOUNT"
vpc_name = "$VPC_NAME"
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
