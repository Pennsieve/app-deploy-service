#!/usr/bin/env sh

root_dir=$(pwd)

exit_status=0
cd "$root_dir/fargate/app-provisioner"
echo "RUNNING fargate/app-provisioner TESTS"
go test -v ./...; exit_status=$((exit_status || $? ))

echo "RUNNING lambda/service TESTS"
cd "$root_dir/lambda/service"
go test -v ./...; exit_status=$((exit_status || $? ))

echo "RUNNING lambda/status TESTS"
cd "$root_dir/lambda/status"
go test -v ./...; exit_status=$((exit_status || $? ))

exit $exit_status