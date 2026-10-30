# BuildKit builder image (SPIKE PROTOTYPE, see docs/buildkit-spike.md).
#
# Rootless BuildKit + the ECR credential helper + a kaniko-compatible entrypoint.
# NOTE: this does NOT run on Fargate: rootless BuildKit needs user namespaces
# (blocked by the default seccomp profile) and Fargate allows neither a custom
# seccomp profile nor CAP_SYS_ADMIN. It runs where seccomp can be relaxed:
# ECS on EC2, or CodeBuild.
FROM moby/buildkit:v0.34.1-rootless

ARG ECR_HELPER_VERSION=0.12.0
ARG TARGETARCH
USER root
RUN wget -q -O /usr/local/bin/docker-credential-ecr-login \
      "https://amazon-ecr-credential-helper-releases.s3.us-east-2.amazonaws.com/${ECR_HELPER_VERSION}/linux-${TARGETARCH}/docker-credential-ecr-login" \
 && chmod 0755 /usr/local/bin/docker-credential-ecr-login
COPY --chmod=0755 build.sh /usr/local/bin/build.sh
USER 1000:1000

# Every ECR registry goes through the helper (env credentials or the task role).
RUN mkdir -p /home/user/.docker \
 && printf '{"credsStore":"ecr-login"}\n' > /home/user/.docker/config.json

ENV BUILDKITD_FLAGS=--oci-worker-no-process-sandbox
ENTRYPOINT ["/usr/local/bin/build.sh"]
