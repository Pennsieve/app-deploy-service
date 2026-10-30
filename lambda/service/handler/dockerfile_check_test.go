package handler

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The step of processor-seizure-dynasd v0.2.0 that kaniko built without its
// body, so /opt/dynasd was never created.
const dynasdV020Dockerfile = `FROM --platform=linux/amd64 python:3.12.16-slim-bookworm

WORKDIR /processor
COPY requirements.txt requirements-${TORCH_FLAVOUR}.txt /processor/
RUN pip install --no-cache-dir --only-binary=:all: \
        -r /processor/requirements.txt -r /processor/requirements-${TORCH_FLAVOUR}.txt

ENV DYNASD_CACHE_DIR=/opt/dynasd
RUN python3 - <<'PY'
import hashlib, os, urllib.request
# copy the checkpoints
for rel, (name, sha) in files.items():
    open(dst, "wb").write(data)
PY

COPY processor/ /processor/processor/
`

func TestBuildKitOnlySyntax(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		want       []string
	}{
		{"dynasd v0.2.0 heredoc", dynasdV020Dockerfile, []string{"line 9: heredoc in RUN (<<)"}},
		{"plain", "FROM alpine\nRUN apk add --no-cache curl \\\n    && echo done\nCOPY . /app\n", nil},
		{"unquoted heredoc", "FROM alpine\nRUN <<EOF\necho hi\nEOF\n", []string{"line 2: heredoc in RUN (<<)"}},
		{"dash heredoc", "FROM alpine\nrun cat <<-\"EOT\" > /x\n\thi\nEOT\n", []string{"line 2: heredoc in RUN (<<)"}},
		{"copy heredoc", "FROM alpine\nCOPY <<EOF /etc/x.conf\nkey=value\nEOF\n", []string{"line 2: heredoc in COPY (<<)"}},
		{"heredoc after a continuation", "FROM alpine\nRUN set -e; \\\n  python3 - <<PY\nprint(1)\nPY\n", []string{"line 2: heredoc in RUN (<<)"}},
		{"bash here-string is fine", "FROM bash\nRUN bash -c 'cat <<< hello'\n", nil},
		{"shift is fine", "FROM bash\nRUN echo $((1<<3))\n", nil},
		{"comment is fine", "FROM alpine\n# RUN <<EOF is BuildKit-only\nRUN true\n", nil},
		{"run mount", "FROM golang\nRUN --mount=type=cache,target=/root/.cache/go-build go build ./...\n", []string{"line 2: RUN --mount"}},
		{"run mount after another flag", "FROM golang\nRUN --network=none --mount=type=secret,id=x cat /run/secrets/x\n", []string{"line 2: RUN --mount"}},
		{"mount as an argument is fine", "FROM alpine\nRUN echo --mount\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildKitOnlySyntax(tt.dockerfile))
		})
	}
}

func TestUnsupportedDockerfileSyntax(t *testing.T) {
	saved := fetchDockerfile
	t.Cleanup(func() { fetchDockerfile = saved })
	fetchDockerfile = func(string, string, string, string) ([]byte, error) { return []byte(dynasdV020Dockerfile), nil }

	t.Setenv(imageBuilderEnv, "")
	reason := unsupportedDockerfileSyntax("https://github.com/org/app", "v1", "", nil)
	assert.Contains(t, reason, "line 9: heredoc in RUN (<<)")
	assert.Contains(t, reason, "kaniko")

	t.Setenv(imageBuilderEnv, "kaniko")
	assert.NotEmpty(t, unsupportedDockerfileSyntax("https://github.com/org/app", "v1", "", nil))

	t.Setenv(imageBuilderEnv, "codebuild")
	assert.Empty(t, unsupportedDockerfileSyntax("https://github.com/org/app", "v1", "", nil), "BuildKit builds heredocs")

	t.Setenv(imageBuilderEnv, "kaniko")
	fetchDockerfile = func(string, string, string, string) ([]byte, error) { return nil, errors.New("rate limited") }
	assert.Empty(t, unsupportedDockerfileSyntax("https://github.com/org/app", "v1", "", nil), "an unreadable Dockerfile doesn't block the release")
	fetchDockerfile = func(string, string, string, string) ([]byte, error) { return nil, nil }
	assert.Empty(t, unsupportedDockerfileSyntax("https://github.com/org/app", "v1", "", nil))
}

// Every Dockerfile a release builds is checked, and the reason names the one
// that kaniko can't build.
func TestUnsupportedDockerfileSyntax_EachBuiltDockerfile(t *testing.T) {
	saved := fetchDockerfile
	t.Cleanup(func() { fetchDockerfile = saved })
	var read []string
	fetchDockerfile = func(_, _, _, file string) ([]byte, error) {
		read = append(read, file)
		if file == "docker/Dockerfile.gpu" {
			return []byte(dynasdV020Dockerfile), nil
		}
		return []byte("FROM python:3.12\nRUN pip install x\n"), nil
	}
	t.Setenv(imageBuilderEnv, "kaniko")

	reason := unsupportedDockerfileSyntax("https://github.com/org/app", "v1", "", []string{"Dockerfile", "docker/Dockerfile.gpu"})
	assert.Equal(t, []string{"Dockerfile", "docker/Dockerfile.gpu"}, read)
	assert.Contains(t, reason, "docker/Dockerfile.gpu: line 9: heredoc in RUN (<<)")

	read = nil
	assert.Empty(t, unsupportedDockerfileSyntax("https://github.com/org/app", "v1", "", []string{"Dockerfile"}))
	assert.Equal(t, []string{"Dockerfile"}, read)
}
