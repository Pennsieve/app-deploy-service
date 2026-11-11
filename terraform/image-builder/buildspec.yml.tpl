version: 0.2

# App Store image build (docs/builds.md). The provisioner starts it with
# CONTEXT, DESTINATION, BUILD_CACHE_REF, DEPLOYMENT_ID, APPLICATION_ID,
# APPLICATIONS_TABLE and, for a private repo, GIT_TOKEN (from SSM); for an
# app variant or app.yml build settings also VARIANT, VARIANT_DEFAULT,
# BUILD_ARGS, BUILD_TARGET, DOCKERFILE and BUILD_CACHE_IMPORT_REF.
env:
  shell: bash

phases:
  install:
    commands:
      # github.com answers 500s for seconds at a time: keep retrying for about
      # a minute (a build lost its BuildKit download to one, 2026-11-07).
      - curl -fsSL --retry 6 --retry-delay 10 --retry-all-errors -o /tmp/buildkit.tgz "https://github.com/moby/buildkit/releases/download/$BUILDKIT_VERSION/buildkit-$BUILDKIT_VERSION.linux-amd64.tar.gz"
      - echo "$BUILDKIT_SHA256  /tmp/buildkit.tgz" | sha256sum -c -
      - tar -C /usr/local -xzf /tmp/buildkit.tgz && rm /tmp/buildkit.tgz
  build:
    commands:
      # buildkitd.sh and build.sh (terraform/image-builder/), gzipped and base64-encoded by Terraform (CodeBuild caps a buildspec at 25,600 characters)
      - echo "${buildkitd_lib}" | base64 -d | gunzip > /tmp/buildkitd.sh
      - echo "${build_script}" | base64 -d | gunzip > /tmp/build.sh
      - sh /tmp/build.sh
