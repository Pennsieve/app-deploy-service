package handler

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
)

// imageBuilderEnv names the environment's App Store image builder: "kaniko"
// (the default) or "codebuild" (BuildKit). See docs/builds.md.
const imageBuilderEnv = "IMAGE_BUILDER"

// kanikoBuildsImages reports whether releases are still built by kaniko.
func kanikoBuildsImages() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(imageBuilderEnv)))
	return v != "codebuild"
}

// heredocPattern matches a Dockerfile heredoc opener: <<EOF, <<-EOF,
// <<"EOF", <<'EOF'. Not a bash here-string (<<<).
var heredocPattern = regexp.MustCompile(`(^|[^<])<<-?\s*["']?[A-Za-z_][A-Za-z0-9_]*["']?`)

// runMountPattern matches a RUN instruction with a --mount flag among its
// leading flags.
var runMountPattern = regexp.MustCompile(`(?i)^RUN\s+(--[a-z-]+(=\S+)?\s+)*--mount[=\s]`)

// buildKitOnlySyntax lists the Dockerfile constructs kaniko does not build
// the way BuildKit (docker build) does, one "line N: what" each. kaniko drops
// heredoc bodies without an error and ignores RUN --mount, so the image it
// pushes is not the one the author tested.
func buildKitOnlySyntax(dockerfile string) []string {
	var problems []string
	for _, inst := range dockerfileInstructions(dockerfile) {
		keyword := strings.ToUpper(strings.Fields(inst.text)[0])
		switch keyword {
		case "RUN", "COPY", "ADD":
			if heredocPattern.MatchString(inst.text) {
				problems = append(problems, fmt.Sprintf("line %d: heredoc in %s (<<)", inst.line, keyword))
			}
		}
		if keyword == "RUN" && runMountPattern.MatchString(inst.text) {
			problems = append(problems, fmt.Sprintf("line %d: RUN --mount", inst.line))
		}
	}
	return problems
}

type dockerfileInstruction struct {
	line int // where the instruction starts (1-based)
	text string
}

// dockerfileInstructions splits a Dockerfile into instructions, joining
// backslash continuations and leaving out comments and blank lines.
func dockerfileInstructions(dockerfile string) []dockerfileInstruction {
	var out []dockerfileInstruction
	var current *dockerfileInstruction
	for i, raw := range strings.Split(dockerfile, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if strings.HasPrefix(line, "#") || (line == "" && current == nil) {
			continue
		}
		continued := strings.HasSuffix(line, "\\")
		line = strings.TrimSuffix(line, "\\")
		if current == nil {
			out = append(out, dockerfileInstruction{line: i + 1})
			current = &out[len(out)-1]
		}
		current.text = strings.TrimSpace(current.text + " " + line)
		if !continued {
			current = nil
		}
	}
	// a trailing continuation, or a blank line inside one, can leave an empty
	// instruction behind
	filtered := out[:0]
	for _, inst := range out {
		if inst.text != "" {
			filtered = append(filtered, inst)
		}
	}
	return filtered
}

// fetchDockerfile reads a Dockerfile of the release (a path in the
// repository). A variable so tests can replace it.
var fetchDockerfile = func(sourceUrl, tag, authToken, file string) ([]byte, error) {
	return newGitHubClient(authToken).GetFileContent(sourceUrl, file, tag)
}

// unsupportedDockerfileSyntax returns why kaniko can't build this release
// faithfully, or "" when it can, when a Dockerfile can't be read (the build
// will say), or when BuildKit builds the environment's images. It checks
// each Dockerfile the release builds (builtDockerfiles): the root
// Dockerfile unless app.yml's build, or a variant's, names others.
func unsupportedDockerfileSyntax(sourceUrl, tag, authToken string, dockerfiles []string) string {
	if !kanikoBuildsImages() {
		return ""
	}
	if len(dockerfiles) == 0 {
		dockerfiles = []string{"Dockerfile"}
	}
	var reasons []string
	for _, file := range dockerfiles {
		dockerfile, err := fetchDockerfile(sourceUrl, tag, authToken, file)
		if err != nil || dockerfile == nil {
			log.Printf("warning: unable to read %s of %s %s to check its syntax: %v", file, sourceUrl, tag, err)
			continue
		}
		problems := buildKitOnlySyntax(string(dockerfile))
		if len(problems) == 0 {
			continue
		}
		if len(dockerfiles) == 1 && file == "Dockerfile" {
			reasons = append(reasons, strings.Join(problems, "; "))
		} else {
			reasons = append(reasons, file+": "+strings.Join(problems, "; "))
		}
	}
	if len(reasons) == 0 {
		return ""
	}
	return fmt.Sprintf("the Dockerfile uses BuildKit-only syntax that the App Store's image builder (kaniko) "+
		"does not build correctly: %s. Rewrite those steps without heredocs or RUN --mount "+
		"(e.g. COPY a script and RUN it), then publish a new release.", strings.Join(reasons, "; "))
}
