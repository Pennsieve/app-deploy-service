package handler

import "testing"

const gpuAppJSON = `{
  "schemaVersion": "1.0.0",
  "application": { "id": "nifti-brain-segmenter" },
  "runtime": {
    "cpu": 4096,
    "memory": 16384,
    "computeTypes": ["gpu", "standard"],
    "timeoutSeconds": 3600
  }
}`

const standardAppJSON = `{
  "application": { "id": "csv-parser" },
  "runtime": { "cpu": 2048, "memory": 4096, "computeTypes": ["standard"] }
}`

const gpuAppYAML = `application:
  id: nifti-brain-segmenter
runtime:
  cpu: 4096
  memory: 16384
  computeTypes:
    - gpu
    - standard
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
		{"gpu json", gpuAppJSON, gpuBuildStorageGiB},
		{"standard json", standardAppJSON, 0},
		{"gpu yaml", gpuAppYAML, gpuBuildStorageGiB},
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

func TestNeedsGPUCaseInsensitive(t *testing.T) {
	var c appConfig
	c.Runtime.ComputeTypes = []string{"Standard", " GPU "}
	if !c.needsGPU() {
		t.Error("needsGPU = false, want true for mixed-case/padded 'GPU'")
	}
}
