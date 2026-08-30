-- CI/CD platform MySQL 8.0 initial schema.
-- Run this file against the database named by MYSQL_DATABASE.

CREATE TABLE IF NOT EXISTS users (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    username VARCHAR(100) NOT NULL,
    display_name VARCHAR(120) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    is_super_admin BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_users_username (username),
    KEY idx_users_admin (is_super_admin),
    CONSTRAINT chk_users_super_admin CHECK (is_super_admin IN (0, 1))
) ENGINE = InnoDB;

CREATE TABLE IF NOT EXISTS spaces (
    id VARCHAR(64) NOT NULL,
    name VARCHAR(120) NOT NULL,
    slug VARCHAR(80) NOT NULL,
    description VARCHAR(255) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_spaces_slug (slug),
    KEY idx_spaces_name (name)
) ENGINE = InnoDB;

CREATE TABLE IF NOT EXISTS space_members (
    space_id VARCHAR(64) NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    role VARCHAR(32) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (space_id, user_id),
    KEY idx_space_members_user (user_id, space_id),
    KEY idx_space_members_role (space_id, role),
    CONSTRAINT fk_space_members_space FOREIGN KEY (space_id) REFERENCES spaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_space_members_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT chk_space_members_role CHECK (role IN ('owner', 'admin', 'developer', 'viewer'))
) ENGINE = InnoDB;

CREATE TABLE IF NOT EXISTS clusters (
    id VARCHAR(64) NOT NULL,
    space_id VARCHAR(64) NOT NULL,
    name VARCHAR(120) NOT NULL,
    provider VARCHAR(32) NOT NULL DEFAULT 'kubernetes',
    api_endpoint VARCHAR(500) NOT NULL DEFAULT '',
    kube_context VARCHAR(255) NOT NULL DEFAULT '',
    connection_mode VARCHAR(32) NOT NULL DEFAULT 'kubeconfig',
    kubeconfig_path VARCHAR(500) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    metadata JSON NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_clusters_space_name (space_id, name),
    UNIQUE KEY uk_clusters_space_id (space_id, id),
    KEY idx_clusters_space_status (space_id, status),
    CONSTRAINT fk_clusters_space FOREIGN KEY (space_id) REFERENCES spaces (id) ON DELETE CASCADE,
    CONSTRAINT chk_clusters_status CHECK (status IN ('active', 'draining', 'offline'))
) ENGINE = InnoDB;

CREATE TABLE IF NOT EXISTS projects (
    id VARCHAR(64) NOT NULL,
    space_id VARCHAR(64) NOT NULL,
    name VARCHAR(120) NOT NULL,
    description VARCHAR(255) NOT NULL DEFAULT '',
    repository_id VARCHAR(255) NOT NULL,
    repository_url VARCHAR(500) NOT NULL,
    default_branch VARCHAR(120) NOT NULL DEFAULT 'main',
    cluster_id VARCHAR(64) NOT NULL,
    namespace VARCHAR(120) NOT NULL,
    deploy_strategy VARCHAR(32) NOT NULL DEFAULT 'rolling',
    replicas INT UNSIGNED NOT NULL DEFAULT 1,
    container_port INT UNSIGNED NOT NULL DEFAULT 8080,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_projects_space_id (space_id, id),
    UNIQUE KEY uk_projects_space_name (space_id, name),
    KEY idx_projects_space_updated (space_id, updated_at),
    KEY idx_projects_cluster (space_id, cluster_id),
    CONSTRAINT fk_projects_space FOREIGN KEY (space_id) REFERENCES spaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_projects_cluster_same_space FOREIGN KEY (space_id, cluster_id)
        REFERENCES clusters (space_id, id) ON DELETE RESTRICT,
    CONSTRAINT chk_projects_strategy CHECK (deploy_strategy IN ('rolling', 'canary', 'blue_green')),
    CONSTRAINT chk_projects_replicas CHECK (replicas > 0),
    CONSTRAINT chk_projects_container_port CHECK (container_port BETWEEN 1 AND 65535)
) ENGINE = InnoDB;

-- A project keeps one source manifest, while each environment deployment
-- target supplies its own cluster, namespace and rollout defaults.
CREATE TABLE IF NOT EXISTS project_deployment_targets (
    id VARCHAR(64) NOT NULL,
    space_id VARCHAR(64) NOT NULL,
    project_id VARCHAR(64) NOT NULL,
    name VARCHAR(120) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    stage VARCHAR(16) NOT NULL DEFAULT 'custom',
    sort_order INT UNSIGNED NOT NULL DEFAULT 1,
    cluster_id VARCHAR(64) NOT NULL,
    namespace VARCHAR(120) NOT NULL,
    replicas INT UNSIGNED NOT NULL DEFAULT 1,
    container_port INT UNSIGNED NOT NULL DEFAULT 8080,
    deploy_strategy VARCHAR(32) NOT NULL DEFAULT 'rolling',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_deployment_targets_space_id (space_id, id),
    UNIQUE KEY uk_deployment_targets_project_name (space_id, project_id, name),
    UNIQUE KEY uk_deployment_targets_project_environment (space_id, project_id, environment),
    KEY idx_deployment_targets_project (space_id, project_id, is_default, enabled),
    KEY idx_deployment_targets_project_order (space_id, project_id, sort_order, enabled),
    KEY idx_deployment_targets_cluster (space_id, cluster_id, namespace),
    CONSTRAINT fk_deployment_targets_project_same_space FOREIGN KEY (space_id, project_id)
        REFERENCES projects (space_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_deployment_targets_cluster_same_space FOREIGN KEY (space_id, cluster_id)
        REFERENCES clusters (space_id, id) ON DELETE RESTRICT,
    CONSTRAINT chk_deployment_targets_strategy CHECK (deploy_strategy IN ('rolling', 'canary', 'blue_green')),
    CONSTRAINT chk_deployment_targets_stage CHECK (stage IN ('dev', 'uat', 'pre', 'prod', 'custom')),
    CONSTRAINT chk_deployment_targets_sort_order CHECK (sort_order > 0),
    CONSTRAINT chk_deployment_targets_replicas CHECK (replicas > 0),
    CONSTRAINT chk_deployment_targets_container_port CHECK (container_port BETWEEN 1 AND 65535),
    CONSTRAINT chk_deployment_targets_status CHECK (status IN ('active', 'draining', 'offline'))
) ENGINE = InnoDB;

-- Native Kubernetes manifests are versioned independently from the project
-- summary so listing projects does not load a potentially large editor value.
CREATE TABLE IF NOT EXISTS project_deployment_configs (
    project_id VARCHAR(64) NOT NULL,
    space_id VARCHAR(64) NOT NULL,
    namespace VARCHAR(120) NOT NULL,
    format VARCHAR(16) NOT NULL DEFAULT 'yaml',
    manifest LONGTEXT NOT NULL,
    version INT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (project_id),
    UNIQUE KEY uk_project_deployment_configs_space_id (space_id, project_id),
    KEY idx_project_deployment_configs_updated (space_id, updated_at),
    CONSTRAINT fk_project_deployment_configs_project_same_space FOREIGN KEY (space_id, project_id)
        REFERENCES projects (space_id, id) ON DELETE CASCADE,
    CONSTRAINT chk_project_deployment_configs_format CHECK (format IN ('yaml', 'json')),
    CONSTRAINT chk_project_deployment_configs_version CHECK (version > 0)
) ENGINE = InnoDB;

CREATE TABLE IF NOT EXISTS project_releases (
    id VARCHAR(64) NOT NULL,
    space_id VARCHAR(64) NOT NULL,
    project_id VARCHAR(64) NOT NULL,
    release_name VARCHAR(120) NOT NULL DEFAULT '',
    source_repository_id VARCHAR(255) NOT NULL,
    source_branch VARCHAR(120) NOT NULL,
    release_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    strategy VARCHAR(32) NOT NULL DEFAULT 'rolling',
    stable_percent TINYINT UNSIGNED NOT NULL DEFAULT 100,
    candidate_percent TINYINT UNSIGNED NOT NULL DEFAULT 0,
    blue_percent TINYINT UNSIGNED NOT NULL DEFAULT 0,
    green_percent TINYINT UNSIGNED NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    created_by BIGINT UNSIGNED NULL,
    published_at DATETIME(3) NULL,
    state_json LONGTEXT NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_project_releases_space_id (space_id, id),
    UNIQUE KEY uk_project_releases_fingerprint (project_id, release_fingerprint),
    KEY idx_project_releases_space_status (space_id, status, created_at),
    KEY idx_project_releases_project_created (project_id, created_at),
    KEY idx_project_releases_creator (created_by, created_at),
    CONSTRAINT fk_project_releases_project_same_space FOREIGN KEY (space_id, project_id)
        REFERENCES projects (space_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_project_releases_creator FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT chk_project_releases_strategy CHECK (strategy IN ('rolling', 'canary', 'blue_green')),
    CONSTRAINT chk_project_releases_status CHECK (status IN ('draft', 'queued', 'running', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT chk_project_releases_percent_range CHECK (
        stable_percent <= 100 AND candidate_percent <= 100 AND blue_percent <= 100 AND green_percent <= 100
    ),
    CONSTRAINT chk_project_releases_traffic CHECK (
        (strategy = 'rolling' AND stable_percent = 100 AND candidate_percent = 0 AND blue_percent = 0 AND green_percent = 0)
        OR (strategy = 'canary' AND stable_percent + candidate_percent = 100 AND blue_percent = 0 AND green_percent = 0)
        OR (strategy = 'blue_green' AND stable_percent = 0 AND candidate_percent = 0 AND blue_percent + green_percent = 100)
    )
) ENGINE = InnoDB;

CREATE TABLE IF NOT EXISTS release_commits (
    space_id VARCHAR(64) NOT NULL,
    release_id VARCHAR(64) NOT NULL,
    commit_sha VARCHAR(128) CHARACTER SET ascii COLLATE ascii_general_ci NOT NULL,
    commit_position INT UNSIGNED NOT NULL,
    is_removed BOOLEAN NOT NULL DEFAULT FALSE,
    removed_at DATETIME(3) NULL,
    removed_by BIGINT UNSIGNED NULL,
    remove_reason VARCHAR(255) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (release_id, commit_sha),
    UNIQUE KEY uk_release_commits_position (release_id, commit_position),
    KEY idx_release_commits_space_active (space_id, release_id, is_removed, commit_position),
    KEY idx_release_commits_sha (commit_sha),
    CONSTRAINT fk_release_commits_release_same_space FOREIGN KEY (space_id, release_id)
        REFERENCES project_releases (space_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_release_commits_removed_by FOREIGN KEY (removed_by) REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT chk_release_commits_removed_fields CHECK (
        (is_removed = 0 AND removed_at IS NULL)
        OR (is_removed = 1 AND removed_at IS NOT NULL)
    )
) ENGINE = InnoDB;

CREATE TABLE IF NOT EXISTS release_batch_state (
    id VARCHAR(64) NOT NULL,
    space_id VARCHAR(64) NOT NULL,
    project_id VARCHAR(64) NOT NULL,
    state LONGTEXT NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_release_batch_state_space_project (space_id, project_id, created_at),
    CONSTRAINT fk_release_batch_state_project_same_space FOREIGN KEY (space_id, project_id)
        REFERENCES projects (space_id, id) ON DELETE CASCADE
) ENGINE = InnoDB;

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    space_id VARCHAR(64) NULL,
    user_id BIGINT UNSIGNED NULL,
    action VARCHAR(80) NOT NULL,
    target_type VARCHAR(80) NOT NULL,
    target_id VARCHAR(128) NOT NULL,
    request_id VARCHAR(128) NOT NULL DEFAULT '',
    metadata JSON NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_audit_logs_space_time (space_id, created_at),
    KEY idx_audit_logs_user_time (user_id, created_at),
    KEY idx_audit_logs_target (target_type, target_id, created_at),
    KEY idx_audit_logs_action_time (action, created_at),
    CONSTRAINT fk_audit_logs_space FOREIGN KEY (space_id) REFERENCES spaces (id) ON DELETE RESTRICT,
    CONSTRAINT fk_audit_logs_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL
) ENGINE = InnoDB;
