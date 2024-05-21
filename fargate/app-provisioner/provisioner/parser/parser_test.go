package parser_test

import (
	"context"
	"testing"

	"github.com/pennsieve/app-deploy-service/app-provisioner/provisioner/parser"
	"github.com/stretchr/testify/assert"
)

func TestOutputParser(t *testing.T) {
	parser := parser.NewOutputParser("./test-data/outputs_test.json")
	outputs, _ := parser.Run(context.Background())
	assert.Equal(t, "some-account-url/app", outputs.AppEcrUrl.Value)
	assert.Equal(t, "some-task-defn-arn", outputs.AppTaskDefn.Value)
}
