version: 0.2

# Fixture bundle build (docs/builds.md "Fixture bundles"). The status lambda
# starts it after an image is promoted, with REPOSITORY, TAG, APPLICATION_ID
# and, optionally, PORTS. The project holds APP_FIXTURES_IMAGE, the bucket
# and the caps.
env:
  shell: bash

phases:
  install:
    commands:
      - curl -fsSL --retry 6 --retry-delay 10 --retry-all-errors -o /tmp/buildkit.tgz "https://github.com/moby/buildkit/releases/download/$BUILDKIT_VERSION/buildkit-$BUILDKIT_VERSION.linux-amd64.tar.gz"
      - echo "$BUILDKIT_SHA256  /tmp/buildkit.tgz" | sha256sum -c -
      - tar -C /usr/local -xzf /tmp/buildkit.tgz && rm /tmp/buildkit.tgz
  build:
    commands:
      # buildkitd.sh and fixtures.sh (terraform/image-builder/), inlined by Terraform
      - echo "${buildkitd_lib}" | base64 -d > /tmp/buildkitd.sh
      - echo "${fixtures_script}" | base64 -d > /tmp/fixtures.sh
      - sh /tmp/fixtures.sh
