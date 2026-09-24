-- Environment-specific full-file replacements for project-level Kubernetes
-- resources. base_global_content is the common ancestor used by TTP's
-- three-way merge when the global file changes later.
CREATE TABLE IF NOT EXISTS project_deployment_resource_overrides (
    id VARCHAR(64) NOT NULL,
    space_id VARCHAR(64) NOT NULL,
    project_id VARCHAR(64) NOT NULL,
    target_id VARCHAR(64) NOT NULL,
    global_resource_id VARCHAR(64) NULL,
    name VARCHAR(255) NOT NULL,
    path VARCHAR(500) NOT NULL,
    format VARCHAR(16) NOT NULL DEFAULT 'yaml',
    content LONGTEXT NOT NULL,
    api_version VARCHAR(120) NOT NULL,
    kind VARCHAR(120) NOT NULL,
    resource_name VARCHAR(253) NOT NULL,
    namespace VARCHAR(120) NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 1,
    version INT UNSIGNED NOT NULL DEFAULT 1,
    release_supported BOOLEAN NOT NULL DEFAULT FALSE,
    base_global_version INT UNSIGNED NOT NULL DEFAULT 0,
    base_global_content LONGTEXT NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_resource_override_path (space_id, project_id, target_id, path),
    UNIQUE KEY uk_resource_override_global (space_id, project_id, target_id, global_resource_id),
    KEY idx_resource_overrides_target_order (space_id, project_id, target_id, sort_order, path),
    CONSTRAINT fk_resource_overrides_project FOREIGN KEY (space_id, project_id)
        REFERENCES projects (space_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_resource_overrides_target FOREIGN KEY (target_id)
        REFERENCES project_deployment_targets (id) ON DELETE CASCADE,
    CONSTRAINT fk_resource_overrides_global FOREIGN KEY (global_resource_id)
        REFERENCES project_deployment_resource_files (id) ON DELETE CASCADE,
    CONSTRAINT chk_resource_overrides_format CHECK (format IN ('yaml', 'json')),
    CONSTRAINT chk_resource_overrides_version CHECK (version > 0)
) ENGINE = InnoDB;
