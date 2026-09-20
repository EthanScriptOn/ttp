-- One independently applicable Kubernetes object per project resource file.
-- Unlike project_deployment_configs, this table never stores a delimiter-
-- separated manifest stream.
CREATE TABLE IF NOT EXISTS project_deployment_resource_files (
    id VARCHAR(64) NOT NULL,
    space_id VARCHAR(64) NOT NULL,
    project_id VARCHAR(64) NOT NULL,
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
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_deployment_resource_files_space_path (space_id, project_id, path),
    UNIQUE KEY uk_deployment_resource_files_space_name (space_id, project_id, name),
    KEY idx_deployment_resource_files_project_order (space_id, project_id, sort_order, path),
    CONSTRAINT fk_deployment_resource_files_project_same_space FOREIGN KEY (space_id, project_id)
        REFERENCES projects (space_id, id) ON DELETE CASCADE,
    CONSTRAINT chk_deployment_resource_files_format CHECK (format IN ('yaml', 'json')),
    CONSTRAINT chk_deployment_resource_files_version CHECK (version > 0)
) ENGINE = InnoDB;
