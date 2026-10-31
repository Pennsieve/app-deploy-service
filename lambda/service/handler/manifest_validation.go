package handler

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/pennsieve/app-deploy-service/service/manifestschema"
	"gopkg.in/yaml.v3"
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
	// What the schema can't say about variants is checked here, in the
	// same "path: reason" form and with the same warn/enforce handling.
	if c, err := parseAppConfig(appYml); err == nil {
		out = append(out, variantProblems(c)...)
		out = append(out, reviewProblems(c)...)
	}
	return out
}

// variantProblems are the checks on app.yml's variants that JSON Schema
// can't express: ids are unique, and each parameter override names a
// top-level parameter whose resulting defaultValue is one of its validValues
// and within its min and max (overridden or inherited).
func variantProblems(c appConfig) []string {
	var out []string
	params := map[string]appParameter{}
	for _, p := range c.Parameters {
		if _, dup := params[p.Name]; p.Name != "" && !dup {
			params[p.Name] = p
		}
	}
	seen := map[string]int{}
	for i, v := range c.Variants {
		path := fmt.Sprintf("/variants/%d", i)
		if first, dup := seen[v.ID]; dup && v.ID != "" {
			out = append(out, fmt.Sprintf("%s/id: %q is already the id of variant %d; variant ids must be unique", path, v.ID, first))
		} else {
			seen[v.ID] = i
		}
		for _, name := range sortedKeys(v.Parameters) {
			o := v.Parameters[name]
			ppath := path + "/parameters/" + jsonPointerEscape(name)
			p, ok := params[name]
			if !ok {
				out = append(out, fmt.Sprintf("%s: there is no top-level parameter %q; a variant can only override existing parameters", ppath, name))
				continue
			}
			if o.DefaultValue == nil && o.ValidValues == nil {
				continue
			}
			eff := p.withOverride(o)
			if eff.DefaultValue == "" && o.DefaultValue == nil {
				continue // no default to check
			}
			if len(eff.ValidValues) > 0 && !containsValue(eff.ValidValues, eff.DefaultValue) {
				out = append(out, fmt.Sprintf("%s/defaultValue: %q is not one of the parameter's validValues %v", ppath, eff.DefaultValue, eff.ValidValues))
			}
			if eff.Min != nil || eff.Max != nil {
				n, err := strconv.ParseFloat(strings.TrimSpace(eff.DefaultValue), 64)
				switch {
				case err != nil:
					out = append(out, fmt.Sprintf("%s/defaultValue: %q is not a number, but the parameter has a min or max", ppath, eff.DefaultValue))
				case eff.Min != nil && n < *eff.Min:
					out = append(out, fmt.Sprintf("%s/defaultValue: %v is less than the parameter's min %v", ppath, n, *eff.Min))
				case eff.Max != nil && n > *eff.Max:
					out = append(out, fmt.Sprintf("%s/defaultValue: %v is more than the parameter's max %v", ppath, n, *eff.Max))
				}
			}
		}
	}
	return out
}

// reviewProblems are the checks on app.yml's review block that JSON Schema
// can't express: its inputs and parameters name the app's own ports and
// parameters, and a review parameter value is one of the
// parameter's validValues and within its min and max.
func reviewProblems(c appConfig) []string {
	if c.Review == nil {
		return nil
	}
	var out []string
	inputs := map[string]bool{}
	for _, p := range c.Inputs {
		inputs[p.Name] = true
	}
	for _, name := range sortedKeys(c.Review.Inputs) {
		if !inputs[name] {
			out = append(out, fmt.Sprintf("/review/inputs/%s: there is no input %q", jsonPointerEscape(name), name))
		}
	}
	params := map[string]appParameter{}
	for _, p := range c.Parameters {
		if _, dup := params[p.Name]; p.Name != "" && !dup {
			params[p.Name] = p
		}
	}
	for _, name := range sortedKeys(c.Review.Parameters) {
		ppath := "/review/parameters/" + jsonPointerEscape(name)
		p, ok := params[name]
		if !ok {
			out = append(out, fmt.Sprintf("%s: there is no parameter %q", ppath, name))
			continue
		}
		node := c.Review.Parameters[name]
		if node.Kind != yaml.ScalarNode {
			continue // the schema reports it
		}
		v := node.Value
		if len(p.ValidValues) > 0 && !containsValue(p.ValidValues, v) {
			out = append(out, fmt.Sprintf("%s: %q is not one of the parameter's validValues %v", ppath, v, p.ValidValues))
		}
		if p.Min != nil || p.Max != nil {
			n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			switch {
			case err != nil:
				out = append(out, fmt.Sprintf("%s: %q is not a number, but the parameter has a min or max", ppath, v))
			case p.Min != nil && n < *p.Min:
				out = append(out, fmt.Sprintf("%s: %v is less than the parameter's min %v", ppath, n, *p.Min))
			case p.Max != nil && n > *p.Max:
				out = append(out, fmt.Sprintf("%s: %v is more than the parameter's max %v", ppath, n, *p.Max))
			}
		}
	}
	return out
}

// containsValue: v is one of values, compared as written or, for numbers,
// by value (1 and 1.0 are the same choice).
func containsValue(values []string, v string) bool {
	n, numErr := strconv.ParseFloat(strings.TrimSpace(v), 64)
	for _, x := range values {
		if x == v {
			return true
		}
		if numErr == nil {
			if m, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil && m == n {
				return true
			}
		}
	}
	return false
}

// jsonPointerEscape escapes a key for a JSON pointer ("~" and "/").
func jsonPointerEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// variantTagProblem refuses a release tag containing "--" for an app with
// variants: "--" separates the tag from the variant id in image tags
// (<hash>-<tag>--<id>). Refused in either validation mode.
func variantTagProblem(c appConfig, tag string) string {
	if len(c.Variants) == 0 || !strings.Contains(tag, "--") {
		return ""
	}
	return fmt.Sprintf("release tag %q contains \"--\", which an app with variants can't use: \"--\" separates the tag from the variant in image tags", tag)
}
