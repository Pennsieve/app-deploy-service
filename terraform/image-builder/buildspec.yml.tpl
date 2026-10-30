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
      - curl -fsSL --retry 3 -o /tmp/buildkit.tgz "https://github.com/moby/buildkit/releases/download/$BUILDKIT_VERSION/buildkit-$BUILDKIT_VERSION.linux-amd64.tar.gz"
      - echo "$BUILDKIT_SHA256  /tmp/buildkit.tgz" | sha256sum -c -
      - tar -C /usr/local -xzf /tmp/buildkit.tgz && rm /tmp/buildkit.tgz
  build:
    commands:
      # build.sh (terraform/image-builder/build.sh), inlined by Terraform
      - echo "${build_script}" | base64 -d > /tmp/build.sh
      - sh /tmp/build.sh
