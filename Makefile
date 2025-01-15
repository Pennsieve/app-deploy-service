.PHONY: help clean test test-ci package publish

LAMBDA_BUCKET ?= "pennsieve-cc-lambda-functions-use1"
WORKING_DIR   ?= "$(shell pwd)"
SERVICE_NAME ?= "app-deploy-service"
PACKAGE_NAME  ?= "${SERVICE_NAME}-${IMAGE_TAG}.zip"
STATUS_PACKAGE_NAME  ?= "${SERVICE_NAME}-status-${IMAGE_TAG}.zip"


.DEFAULT: help

help:
	@echo "Make Help for $(SERVICE_NAME)"
	@echo ""
	@echo "make clean			- spin down containers and remove db files"
	@echo "make test			- run dockerized tests locally"
	@echo "make test-ci			- run dockerized tests for Jenkins"
	@echo "make package			- create venv and package lambda function"
	@echo "make publish			- package and publish lambda function"

# Run dockerized tests (can be used locally)
test:
	docker compose -f docker-compose.test.yml down --remove-orphans
	docker compose -f docker-compose.test.yml up --exit-code-from local_tests local_tests
	make clean

# Run dockerized tests (used on Jenkins)
test-ci:
	docker compose -f docker-compose.test.yml down --remove-orphans
	@IMAGE_TAG=$(IMAGE_TAG) docker compose -f docker-compose.test.yml up --exit-code-from=ci-tests ci-tests

# Remove folders created by NEO4J docker container
clean: docker-clean
	rm -rf conf
	rm -rf data
	rm -rf plugins

# Spin down active docker containers.
docker-clean:
	docker compose -f docker-compose.test.yml down

# Build lambda and create ZIP file
package:
	@echo ""
	@echo "*******************************"
	@echo "*   Building service lambda   *"
	@echo "*******************************"
	@echo ""
	cd lambda/service; \
  		env GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o $(WORKING_DIR)/lambda/bin/service/bootstrap; \
		cd $(WORKING_DIR)/lambda/bin/service/ ; \
			zip -r $(WORKING_DIR)/lambda/bin/service/$(PACKAGE_NAME) .
	@echo ""
	@echo "******************************"
	@echo "*   Building status lambda   *"
	@echo "******************************"
	@echo ""
	cd lambda/status; \
  		env GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o $(WORKING_DIR)/lambda/bin/status/bootstrap; \
		cd $(WORKING_DIR)/lambda/bin/status/ ; \
			zip -r $(WORKING_DIR)/lambda/bin/status/$(STATUS_PACKAGE_NAME) .
	@echo ""
	@echo "***********************"
	@echo "*   Building Fargate   *"
	@echo "***********************"
	@echo ""
	cd $(WORKING_DIR)/fargate/app-provisioner; \
		docker build -t pennsieve/app-provisioner:${IMAGE_TAG} . ;\

	@echo "Done"		

# Copy Service lambda to S3 location
publish:
	@make package
	@echo ""
	@echo "*********************************"
	@echo "*   Publishing service lambda   *"
	@echo "*********************************"
	@echo ""
	@echo "starting cp"
	ls $(WORKING_DIR)/lambda/bin/service/
	aws s3 cp $(WORKING_DIR)/lambda/bin/service/$(PACKAGE_NAME) s3://$(LAMBDA_BUCKET)/$(SERVICE_NAME)/ --output json
	@echo "done cp"
	rm -rf $(WORKING_DIR)/lambda/bin/service/$(PACKAGE_NAME) $(WORKING_DIR)/lambda/bin/service/bootstrap
	@make package
	@echo ""
	@echo "********************************"
	@echo "*   Publishing status lambda   *"
	@echo "********************************"
	@echo ""
	@echo "starting cp"
	ls $(WORKING_DIR)/lambda/bin/status/
	aws s3 cp $(WORKING_DIR)/lambda/bin/status/$(STATUS_PACKAGE_NAME) s3://$(LAMBDA_BUCKET)/$(SERVICE_NAME)/ --output json
	@echo "done cp"
	rm -rf $(WORKING_DIR)/lambda/bin/status/$(STATUS_PACKAGE_NAME) $(WORKING_DIR)/lambda/bin/status/bootstrap
	@echo ""
	@echo "**************************"
	@echo "*   Publishing Fargate   *"
	@echo "**************************"
	@echo ""
	docker push pennsieve/app-provisioner:${IMAGE_TAG}
	@echo "Done"

# Run go mod tidy on modules
tidy:
	cd ${WORKING_DIR}/lambda/service; go mod tidy
	cd ${WORKING_DIR}/lambda/status; go mod tidy

