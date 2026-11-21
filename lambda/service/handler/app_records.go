package handler

import (
	"fmt"
	"strings"
)

// recordsStructure is the format registry's structure of a dataset's
// metadata records (docs/app-manifest-records.md): an input that names it
// first in conformsTo is filled with records from a dataset view.
const recordsStructure = "pennsieve-records"

// takesRecords reports whether a port's outermost structure is
// pennsieve-records, with or without a version (pennsieve-records@1.0),
// given its conformsTo as stored (trimmed, blank entries dropped).
// workflow-service decides the same for a run's links
// (internal/handler/port_links.go); keep the two in step.
func takesRecords(conformsTo []string) bool {
	if len(conformsTo) == 0 {
		return false
	}
	id, _, _ := strings.Cut(conformsTo[0], "@")
	return id == recordsStructure
}

// recordsProblems are the checks on records ports that JSON Schema can't
// express: an input of records goes in its own folder (placement:
// subfolder), since its layout would mix with other inputs at the root, and
// no output writes records yet. They judge the ports as they are stored
// (conformsTo and placement trimmed, as appPorts stores them), which is
// what workflow-service reads. Refused in either validation mode
// (alwaysRefused).
func recordsProblems(c appConfig) []string {
	var out []string
	for i, p := range c.Inputs {
		if takesRecords(p.ConformsTo.trimmed()) && strings.TrimSpace(p.Placement) != "subfolder" {
			out = append(out, fmt.Sprintf("/inputs/%d/placement: an input of metadata records (conformsTo: %s) needs placement: subfolder: the records go in its own folder", i, recordsStructure))
		}
	}
	for i, p := range c.Outputs {
		if takesRecords(p.ConformsTo.trimmed()) {
			out = append(out, fmt.Sprintf("/outputs/%d/conformsTo: apps can't write metadata records (%s) yet", i, recordsStructure))
		}
	}
	return out
}
