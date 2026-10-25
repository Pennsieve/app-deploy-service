package handler

import (
	"log"
	"os"
	"strings"

	"github.com/pennsieve/app-deploy-service/service/manifestschema"
)

// How registration treats an app.yml that doesn't follow the schema
// (APP_MANIFEST_VALIDATION): "warn" (the default) registers it and records
// the problems on the version; "enforce" refuses the release with them.
const (
	manifestValidationWarn    = "warn"
	manifestValidationEnforce = "enforce"
)

func manifestValidationMode() string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_MANIFEST_VALIDATION")), manifestValidationEnforce) {
		return manifestValidationEnforce
	}
	return manifestValidationWarn
}

// manifestProblems checks app.yml text against the schema, as "path: reason"
// lines. A file that isn't YAML at all is one problem; a schema that can't be
// loaded is logged and not held against the app.
func manifestProblems(appYml []byte) []string {
	ps, err := manifestschema.Validate(appYml)
	if err != nil {
		if strings.Contains(err.Error(), "YAML") {
			return []string{err.Error()}
		}
		log.Printf("warning: app.yml not validated: %v", err)
		return nil
	}
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return out
}
