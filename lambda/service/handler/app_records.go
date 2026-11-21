package handler

import (
	"fmt"
	"strings"
)

// viewPartStructure is a registry structure whose inputs a run fills with a
// part of a dataset view, not with files: an input that names it first in
// conformsTo takes that part. One row of the part table that
// workflow-service (internal/handler/port_links.go viewParts), the
// provisioner and the app keep in step.
type viewPartStructure struct {
	// ID is the registry id; What and Goes name the part in messages.
	ID, What, Goes string
}

// viewPartStructures are the structures of a dataset's metadata records
// (docs/app-manifest-records.md) and of its knowledge graph
// (docs/app-manifest-kgx.md).
var viewPartStructures = []viewPartStructure{
	{ID: "pennsieve-records", What: "metadata records", Goes: "the records go"},
	{ID: "kgx", What: "a knowledge graph", Goes: "the graph goes"},
}

// viewPartOf is the view part a port takes: the row for its outermost
// structure, with or without a version (pennsieve-records@1.0), given its
// conformsTo as stored (trimmed, blank entries dropped). workflow-service
// decides the same for a run's links; keep the two in step.
func viewPartOf(conformsTo []string) (viewPartStructure, bool) {
	if len(conformsTo) == 0 {
		return viewPartStructure{}, false
	}
	id, _, _ := strings.Cut(conformsTo[0], "@")
	for _, s := range viewPartStructures {
		if s.ID == id {
			return s, true
		}
	}
	return viewPartStructure{}, false
}

// recordsProblems are the checks on ports of view parts (records, a
// knowledge graph) that JSON Schema can't express: such an input goes in its
// own folder (placement: subfolder), since its layout would mix with other
// inputs at the root, and no output writes one yet. They judge the ports as
// they are stored (conformsTo and placement trimmed, as appPorts stores
// them), which is what workflow-service reads. Refused in either validation
// mode (alwaysRefused).
func recordsProblems(c appConfig) []string {
	var out []string
	for i, p := range c.Inputs {
		if s, ok := viewPartOf(p.ConformsTo.trimmed()); ok && strings.TrimSpace(p.Placement) != "subfolder" {
			out = append(out, fmt.Sprintf("/inputs/%d/placement: an input of %s (conformsTo: %s) needs placement: subfolder: %s in its own folder", i, s.What, s.ID, s.Goes))
		}
	}
	for i, p := range c.Outputs {
		if s, ok := viewPartOf(p.ConformsTo.trimmed()); ok {
			out = append(out, fmt.Sprintf("/outputs/%d/conformsTo: apps can't write %s (%s) yet", i, s.What, s.ID))
		}
	}
	return out
}
