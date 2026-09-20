# TTP Builder

`ttp-builder` is an optional, detached image-build worker that lives in the
same TTP source repository and can run on the same machine as the API. It is
still a separate process: the TTP API neither invokes Docker nor receives the
registry password.

The worker uses [BuildKit](https://github.com/moby/buildkit), checks out the
exact commit stored on a release, builds the `Dockerfile` at the repository
root with the repository root as context, pushes to the platform-owned OCI
registry, and returns `repository@sha256:...`. Every environment in one release
execution deploys that same digest.

## Registry contract

TTP uses the standard OCI/Docker Registry interface. It does not contain an
阿里云、Harbor、GitLab or Docker Hub push implementation. The builder accepts
any reachable registry that supports the normal `/v2/` authentication flow,
including Basic and Bearer authentication.

The platform can forward a selected, space-owned connection for one build. A
project only selects that connection; the builder derives a stable repository
path from the registry host and project ID. For backwards-compatible API
clients that still provide an image repository, the builder verifies that the
forwarded credential's host matches it and refuses a mismatch. For projects
without a connection, configure one credential entry per registry host in the
builder's private credential map. Repository values must not contain a tag or
digest.

Push and pull credentials are separate concerns:

- Builder credential: lets BuildKit push the newly built image.
- Kubernetes `imagePullSecrets`: lets cluster nodes pull that image.

TTP checks the builder-side registry credential before a release starts, but
the actual push remains the final authority. A registry can still reject a
specific repository or operation during upload, and that error is recorded in
the release logs.

## Boundaries

- TTP supports any HTTP(S) Git source allowed by the worker's host allowlist;
  GitHub and GitLab are not special cases.
- A project selects a space-owned image registry connection in project
  settings. The worker derives a stable repository path from that connection
  and the project ID, so project users never need to enter a registry path or
  receive registry passwords. Explicit image repository values remain
  accepted for backwards-compatible API clients. Without a project
  connection, the tag policy, registry and target platforms stay configured
  by TTP platform operators.
- Registry credentials selected in the console are encrypted in the TTP
  database and forwarded only for the lifetime of a build; they are never
  exposed to project users. The builder-host mapping remains the fallback for
  platform-owned repositories and is never returned by the API.
- A project Git robot token is forwarded only for a single build request. It is
  never included in the image, artifact record, browser response, or build log.
- For a project connection, TTP creates/updates a namespaced
  `kubernetes.io/dockerconfigjson` Secret and injects its name into each
  Deployment. Existing manifest `imagePullSecrets` are preserved. The TTP
  Kubernetes identity therefore needs Secret create/update permission in each
  target namespace.

## Prerequisites

Install `git`, `buildctl`, and `buildkitd` on the builder machine. BuildKit
must be reachable through a dedicated endpoint. For a local Unix socket:

```bash
mkdir -p "$HOME/.local/share/ttp-builder"
buildkitd --addr unix://$HOME/.local/share/ttp-builder/buildkitd.sock
```

Run BuildKit as a dedicated low-privilege OS user in production. Do not expose
the BuildKit endpoint to the public network. For a remote endpoint, put it on
a private network and use the BuildKit deployment's own TLS/authentication
controls.

Create a private credential mapping on the builder machine. The map contains
one entry per registry host. Copy the example and replace its placeholder
values locally:

```bash
install -d -m 700 "$HOME/.config/ttp-builder"
cp deploy/builder/registry-credentials.example.json "$HOME/.config/ttp-builder/registry-credentials.json"
chmod 600 "$HOME/.config/ttp-builder/registry-credentials.json"
```

The file must be a regular file with mode `0600`; the worker refuses more
permissive files. Do not commit a real credential file. The selected default
credential reference is a platform environment setting, not a project setting.
For a project-specific repository, the registry host is matched automatically
to its entry.

## Same-machine startup

Set the standard TTP server variables, then add the builder variables below.
The local startup script launches `cmd/builder` next to the backend and
frontend, while keeping it as a separate OS process.

```bash
export TTP_BUILDER_ENABLED=true
export TTP_BUILDER_TOKEN="$(openssl rand -hex 32)"
export TTP_BUILDER_BUILDKIT_ADDR="unix://$HOME/.local/share/ttp-builder/buildkitd.sock"
export TTP_BUILDER_REGISTRY_CREDENTIALS_FILE="$HOME/.config/ttp-builder/registry-credentials.json"
# Only for explicitly trusted local HTTP registries. Omit for production HTTPS registries.
export TTP_BUILDER_INSECURE_REGISTRIES='docker.for.mac.localhost:5000'
export TTP_BUILDER_ALLOWED_GIT_HOSTS='git.example.com,github.com,gitlab.com'
export TTP_BUILDER_IMAGE_REPOSITORY_PREFIX='registry.example.com/ttp'
export TTP_BUILDER_REGISTRY_CREDENTIAL_REF='platform-registry'
export TTP_BUILDER_WORKDIR="$HOME/.local/share/ttp-builder/work"

# Optional. Keep false when buildkitd is supervised separately.
export TTP_BUILDER_START_BUILDKIT=false

scripts/start-local.sh
```

When `TTP_BUILDER_ENABLED=true`, `scripts/start-local.sh` automatically starts
the worker on `127.0.0.1:8791`, sets `CICD_IMAGE_BUILDER_URL` to that local
address, and uses `TTP_BUILDER_TOKEN` for the API-to-worker call.
`TTP_BUILDER_ADDR` may use another loopback address and port. For a worker on
another machine, leave
`TTP_BUILDER_ENABLED` disabled and configure an HTTPS
`CICD_IMAGE_BUILDER_URL` directly.

To let the script own a local daemon as well, set
`TTP_BUILDER_START_BUILDKIT=true`. It then starts `buildkitd` as a third
process and stops it with TTP. For service managers, run BuildKit separately
and keep that option false.

## Project contract

A project user only commits a file named `Dockerfile` at the repository root.
TTP checks out the release commit, uses that file and the repository root as
the build context, derives the platform-owned image repository, and pushes the
image with the platform registry credential. There is no project build form or
project build configuration record.

On publish, TTP creates a traceable `sha-<commit>` tag for the push and deploys
only the digest returned by BuildKit. Re-publish rebuilds the release's saved
commit; it never silently reads a newer branch head.
