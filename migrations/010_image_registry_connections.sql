-- Space-scoped OCI registry connections. Credentials are encrypted by the
-- TTP API before they reach this table and are never returned to clients.
CREATE TABLE IF NOT EXISTS image_registry_connections (
    id VARCHAR(64) NOT NULL,
    space_id VARCHAR(64) NOT NULL,
    name VARCHAR(120) NOT NULL,
    registry VARCHAR(255) NOT NULL,
    auth_type VARCHAR(32) NOT NULL DEFAULT 'basic',
    username VARCHAR(120) NOT NULL DEFAULT '',
    pull_secret_name VARCHAR(120) NOT NULL,
    credential_ciphertext LONGTEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'unverified',
    last_checked_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_image_registry_connections_space_id (space_id, id),
    UNIQUE KEY uk_image_registry_connections_space_name (space_id, name),
    UNIQUE KEY uk_image_registry_connections_space_registry (space_id, registry),
    KEY idx_image_registry_connections_space_status (space_id, status, updated_at),
    CONSTRAINT fk_image_registry_connections_space FOREIGN KEY (space_id)
        REFERENCES spaces (id) ON DELETE CASCADE,
    CONSTRAINT chk_image_registry_connections_auth_type CHECK (auth_type IN ('basic', 'token')),
    CONSTRAINT chk_image_registry_connections_status CHECK (status IN ('unverified', 'active', 'invalid'))
) ENGINE = InnoDB;

DROP PROCEDURE IF EXISTS cicd_add_project_registry_connection;
DELIMITER $$
CREATE PROCEDURE cicd_add_project_registry_connection()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'projects' AND column_name = 'registry_connection_id'
    ) THEN
        ALTER TABLE projects ADD COLUMN registry_connection_id VARCHAR(64) NULL AFTER image_repository;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.statistics
        WHERE table_schema = DATABASE() AND table_name = 'projects' AND index_name = 'idx_projects_registry_connection'
    ) THEN
        ALTER TABLE projects ADD KEY idx_projects_registry_connection (space_id, registry_connection_id);
    END IF;
END$$
DELIMITER ;
CALL cicd_add_project_registry_connection();
DROP PROCEDURE cicd_add_project_registry_connection;

-- The composite reference prevents a project from selecting another space's
-- connection. Existing projects remain unbound because the new column is NULL.
SET @cicd_registry_fk_exists = (
    SELECT COUNT(*) FROM information_schema.referential_constraints
    WHERE constraint_schema = DATABASE() AND table_name = 'projects' AND constraint_name = 'fk_projects_registry_connection_same_space'
);
SET @cicd_registry_fk_sql = IF(@cicd_registry_fk_exists = 0,
    'ALTER TABLE projects ADD CONSTRAINT fk_projects_registry_connection_same_space FOREIGN KEY (space_id, registry_connection_id) REFERENCES image_registry_connections (space_id, id) ON DELETE RESTRICT',
    'SELECT 1');
PREPARE cicd_registry_fk_statement FROM @cicd_registry_fk_sql;
EXECUTE cicd_registry_fk_statement;
DEALLOCATE PREPARE cicd_registry_fk_statement;
SET @cicd_registry_fk_exists = NULL;
SET @cicd_registry_fk_sql = NULL;
