-- Durable release execution state. The release service keeps a short-lived
-- cache, but the database is the source of truth across process restarts.

DROP PROCEDURE IF EXISTS cicd_add_column_if_missing;
DELIMITER $$
CREATE PROCEDURE cicd_add_column_if_missing(
    IN p_table_name VARCHAR(64),
    IN p_column_name VARCHAR(64),
    IN p_alter_sql VARCHAR(1000)
)
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = DATABASE()
          AND table_name = p_table_name
          AND column_name = p_column_name
    ) THEN
        SET @cicd_alter_sql = p_alter_sql;
        PREPARE cicd_alter_statement FROM @cicd_alter_sql;
        EXECUTE cicd_alter_statement;
        DEALLOCATE PREPARE cicd_alter_statement;
    END IF;
END$$
DELIMITER ;

CALL cicd_add_column_if_missing('project_releases', 'progress', 'ALTER TABLE project_releases ADD COLUMN progress INT UNSIGNED NOT NULL DEFAULT 0 AFTER status');
CALL cicd_add_column_if_missing('project_releases', 'stage', 'ALTER TABLE project_releases ADD COLUMN stage VARCHAR(80) NOT NULL DEFAULT '''' AFTER progress');
CALL cicd_add_column_if_missing('project_releases', 'message', 'ALTER TABLE project_releases ADD COLUMN message TEXT NULL AFTER stage');
CALL cicd_add_column_if_missing('project_releases', 'error', 'ALTER TABLE project_releases ADD COLUMN error TEXT NULL AFTER message');
CALL cicd_add_column_if_missing('project_releases', 'started_at', 'ALTER TABLE project_releases ADD COLUMN started_at DATETIME(3) NULL AFTER error');
CALL cicd_add_column_if_missing('project_releases', 'finished_at', 'ALTER TABLE project_releases ADD COLUMN finished_at DATETIME(3) NULL AFTER started_at');
CALL cicd_add_column_if_missing('release_commits', 'short_sha', 'ALTER TABLE release_commits ADD COLUMN short_sha VARCHAR(32) NOT NULL DEFAULT '''' AFTER remove_reason');
CALL cicd_add_column_if_missing('release_commits', 'commit_message', 'ALTER TABLE release_commits ADD COLUMN commit_message TEXT NULL AFTER short_sha');
CALL cicd_add_column_if_missing('release_commits', 'author', 'ALTER TABLE release_commits ADD COLUMN author VARCHAR(255) NOT NULL DEFAULT '''' AFTER commit_message');
CALL cicd_add_column_if_missing('release_commits', 'authored_at', 'ALTER TABLE release_commits ADD COLUMN authored_at DATETIME(3) NULL AFTER author');

DROP PROCEDURE cicd_add_column_if_missing;

CREATE TABLE IF NOT EXISTS release_runtime_targets (
    space_id VARCHAR(64) NOT NULL,
    release_id VARCHAR(64) NOT NULL,
    target_id VARCHAR(64) NOT NULL,
    name VARCHAR(120) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    environment_stage VARCHAR(16) NOT NULL DEFAULT 'custom',
    sort_order INT NOT NULL DEFAULT 1,
    cluster_id VARCHAR(64) NOT NULL,
    namespace VARCHAR(120) NOT NULL,
    replicas INT UNSIGNED NOT NULL DEFAULT 1,
    container_port INT UNSIGNED NOT NULL DEFAULT 8080,
    deploy_strategy VARCHAR(32) NOT NULL DEFAULT 'rolling',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    progress INT UNSIGNED NOT NULL DEFAULT 0,
    stage VARCHAR(80) NOT NULL DEFAULT '',
    message TEXT NULL,
    error TEXT NULL,
    started_at DATETIME(3) NULL,
    finished_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (release_id, target_id),
    KEY idx_release_runtime_targets_space_status (space_id, status, updated_at),
    KEY idx_release_runtime_targets_target (space_id, target_id, updated_at),
    CONSTRAINT fk_release_runtime_targets_release_same_space FOREIGN KEY (space_id, release_id)
        REFERENCES project_releases (space_id, id) ON DELETE CASCADE,
    CONSTRAINT chk_release_runtime_targets_status CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT chk_release_runtime_targets_progress CHECK (progress <= 100),
    CONSTRAINT chk_release_runtime_targets_replicas CHECK (replicas > 0),
    CONSTRAINT chk_release_runtime_targets_container_port CHECK (container_port BETWEEN 1 AND 65535)
) ENGINE = InnoDB;

CREATE TABLE IF NOT EXISTS release_execution_logs (
    id VARCHAR(128) NOT NULL,
    space_id VARCHAR(64) NOT NULL,
    release_id VARCHAR(64) NOT NULL,
    target_id VARCHAR(64) NOT NULL,
    logged_at DATETIME(3) NOT NULL,
    source VARCHAR(32) NOT NULL,
    stream VARCHAR(32) NOT NULL,
    level VARCHAR(16) NOT NULL,
    line TEXT NOT NULL,
    PRIMARY KEY (id),
    KEY idx_release_execution_logs_target_time (space_id, release_id, target_id, logged_at, id),
    CONSTRAINT fk_release_execution_logs_release_same_space FOREIGN KEY (space_id, release_id)
        REFERENCES project_releases (space_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_release_execution_logs_target FOREIGN KEY (release_id, target_id)
        REFERENCES release_runtime_targets (release_id, target_id) ON DELETE CASCADE
) ENGINE = InnoDB;
