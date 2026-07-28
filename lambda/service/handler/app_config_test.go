package handler

import (
	"reflect"
	"testing"

	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
)

const gpuAppYAML = `schemaVersion: 1.0.0
application:
  id: gpu-app
  name: gpu-app
  description: gpu-app
  version: 1.0.1
  type: processor
  maintainers:
    - name: edmore
  tags:
    - pytorch
    - gpu
    - demo
runtime:
  cpu: 1024
  memory: 2048
  computeTypes:
    - gpu
  timeoutSeconds: 300
parameters: []
commandArguments: []
inputs:
  - name: package_file
    description: Pipeline package file.
    mediaTypes:
      - application/octet-stream
    cardinality: one
    required: true
outputs:
  - name: package_file
    description: Pipeline package file.
    mediaTypes:
      - application/octet-stream
`

const standardAppYAML = `application:
  id: csv-parser
runtime:
  cpu: 2048
  memory: 4096
  computeTypes:
    - standard
`

const gpuFlowStyleYAML = `application: {id: nifti-brain-segmenter}
runtime:
  computeTypes: [gpu, standard]
`

const noRuntimeYAML = `application:
  id: hello-world
`

func TestBuildStorageGiB(t *testing.T) {
	tests := []struct {
		name string
		data string
		want int32
	}{
		{"gpu yaml", gpuAppYAML, gpuBuildStorageGiB},
		{"standard yaml", standardAppYAML, 0},
		{"gpu flow-style yaml", gpuFlowStyleYAML, gpuBuildStorageGiB},
		{"no runtime", noRuntimeYAML, 0},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parseAppConfig([]byte(tt.data))
			if err != nil {
				t.Fatalf("parseAppConfig: %v", err)
			}
			if got := buildStorageGiB(c); got != tt.want {
				t.Errorf("buildStorageGiB = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseAppConfigMalformed(t *testing.T) {
	if _, err := parseAppConfig([]byte("runtime: [oops")); err == nil {
		t.Fatal("expected error for malformed app.yml, got nil")
	}
}

const paramsAppYAML = `schemaVersion: 1.0.0
application:
  id: spike-detector
  name: Spike Detector
  type: processor
runtime:
  cpu: 4096
  memory: 16384
  computeTypes:
    - standard
parameters:
  - name: threshold
    type: number
    description: detection threshold
    defaultValue: "0.5"
    validValues:
      - "0.1"
      - "0.5"
      - "0.9"
  - name: channel
    type: string
    description: channel to analyze
commandArguments: []
`

func TestAppParameters(t *testing.T) {
	tests := []struct {
		name string
		data string
		want []store_dynamodb.AppParameter
	}{
		{
			name: "populated parameters",
			data: paramsAppYAML,
			want: []store_dynamodb.AppParameter{
				{
					Name:         "threshold",
					Type:         "number",
					Description:  "detection threshold",
					DefaultValue: "0.5",
					ValidValues:  []string{"0.1", "0.5", "0.9"},
				},
				{
					Name:        "channel",
					Type:        "string",
					Description: "channel to analyze",
				},
			},
		},
		{"empty parameters list", gpuAppYAML, nil},
		{"no parameters key", standardAppYAML, nil},
		{"empty document", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parseAppConfig([]byte(tt.data))
			if err != nil {
				t.Fatalf("parseAppConfig: %v", err)
			}
			got := appParameters(c)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("appParameters = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAppParametersSkipsUnnamed(t *testing.T) {
	const yaml = `parameters:
  - name: ""
    type: string
  - name: keep
    type: string
`
	c, err := parseAppConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("parseAppConfig: %v", err)
	}
	got := appParameters(c)
	if len(got) != 1 || got[0].Name != "keep" {
		t.Errorf("appParameters = %+v, want single 'keep' entry", got)
	}
}

func TestNeedsGPUCaseInsensitive(t *testing.T) {
	var c appConfig
	c.Runtime.ComputeTypes = []string{"Standard", " GPU "}
	if !c.needsGPU() {
		t.Error("needsGPU = false, want true for mixed-case/padded 'GPU'")
	}
}
