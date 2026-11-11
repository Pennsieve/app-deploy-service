version: 0.2

# Sample run (docs/builds.md "Sample runs"). The status lambda starts it once
# a release's fixtures are ready, with IMAGE, BUNDLE_KEY, APPLICATION_ID, TAG,
# PARAMETERS and MEMORY_MB. The project holds the bucket and the caps. Docker
# is on the standard image already; nothing to install.
env:
  shell: bash

phases:
  build:
    commands:
      # sample-run.sh (terraform/image-builder/), gzipped and base64-encoded by Terraform
      - echo "${sample_script}" | base64 -d | gunzip > /tmp/sample-run.sh
      - bash /tmp/sample-run.sh
