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

## Boundaries

- TTP supports any HTTP(S) Git source allowed by the worker's host allowlist;
  GitHub and GitLab are not special cases.
- A project may specify its own image repository in project settings
  (`registry.example.com/team/app`, no tag). The worker only accepts it when
  the repository's registry host matches one of the credentials in the
  builder-host credential mapping, so projects can choose a repository path
  but never receive or provide registry passwords. Without a project
  override, the tag policy, registry and target platforms stay configured by
  TTP platform operators.
- Registry passwords exist only in the builder-host credential mapping. They
  are never stored in the TTP database or exposed to project users.
- A project Git robot token is forwarded only for a single build request. It is
  never included in the image, artifact record, browser response, or build log.
- Kubernetes image pull credentials remain a cluster administrator concern;
  configure `imagePullSecrets` in the project's Kubernetes manifest or
  namespace as appropriate.

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

Create a private credential mapping on the builder machine. The map key is the
reference entered in TTP project settings. Copy the example and replace its
placeholder values locally:

```bash
install -d -m 700 "$HOME/.config/ttp-builder"
cp deploy/builder/registry-credentials.example.json "$HOME/.config/ttp-builder/registry-credentials.json"
chmod 600 "$HOME/.config/ttp-builder/registry-credentials.json"
```

The file must be a regular file with mode `0600`; the worker refuses more
permissive files. Do not commit a real credential file. The reference is a
platform environment setting, not a project setting.

## Same-machine startup

Set the standard TTP server variables, then add the builder variables below.
The local startup script launches `cmd/builder` next to the backend and
frontend, while keeping it as a separate OS process.

```bash
export TTP_BUILDER_ENABLED=true
export TTP_BUILDER_TOKEN="$(openssl rand -hex 32)"
export TTP_BUILDER_BUILDKIT_ADDR="unix://$HOME/.local/share/ttp-builder/buildkitd.sock"
export TTP_BUILDER_REGISTRY_CREDENTIALS_FILE="$HOME/.config/ttp-builder/registry-credentials.json"
export TTP_BUILDER_ALLOWED_GIT_HOSTS='git.example.com,github.com,gitlab.com'
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
