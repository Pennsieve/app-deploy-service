package models

type PennsieveConfig struct {
	ExecutionTargets []string `json:"executionTargets,omitempty"`
	DefaultCPU       int      `json:"defaultCPU,omitempty"`
	DefaultMemory    int      `json:"defaultMemory,omitempty"`
}
