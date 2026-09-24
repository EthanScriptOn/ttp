# Changelog

All notable changes to TTP are documented here.

## [0.2.0] - 2026-09-24

### Added

- Kubernetes deployment resource files with environment-aware overrides.
- Namespace quotas, `LimitRange` defaults, PVC validation, and deployment resource checks.
- Release flows, immutable release history, deployment targets, and release progress tracking.
- Project-level RBAC and release-operation permissions.
- BuildKit image build and push integration with registry and Kubernetes connectivity checks.
- Prometheus-oriented cluster monitoring and Kubernetes registry pull probes.
- Pod terminal and Pod log access.
- Search, filtering, pagination, and normalized summaries for release logs and history.
- Environment-triggered project auto-merge configuration.

### Improved

- Release workflow, environment status, deployment configuration, and runtime log presentation.
- Git provider credential handling, repository access checks, and resilient network retries.
- Streaming build and deployment log normalization, including Git progress output.
- Local development startup and domestic image mirror defaults.

### Fixed

- Release status, deployment target, current-flow, and release-history inconsistencies.
- Git stderr progress output being incorrectly reported as an error.
- Runtime rollout readiness and image-pull validation issues.
- Duplicate release fingerprints and release pagination edge cases.

## [0.1.0] - 2026-09-11

- Initial open-source release of TTP.
