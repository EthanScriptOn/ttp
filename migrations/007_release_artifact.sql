-- One release execution uses one immutable image digest for every selected
-- environment. A new execution clears and replaces these columns.
DROP PROCEDURE IF EXISTS cicd_add_release_artifact_column;
DELIMITER $$
CREATE PROCEDURE cicd_add_release_artifact_column(
    IN p_column_name VARCHAR(64),
    IN p_alter_sql VARCHAR(1000)
)
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'project_releases' AND column_name = p_column_name
    ) THEN
        SET @cicd_release_artifact_sql = p_alter_sql;
        PREPARE cicd_release_artifact_statement FROM @cicd_release_artifact_sql;
        EXECUTE cicd_release_artifact_statement;
        DEALLOCATE PREPARE cicd_release_artifact_statement;
    END IF;
END$$
DELIMITER ;

CALL cicd_add_release_artifact_column('image_ref', 'ALTER TABLE project_releases ADD COLUMN image_ref VARCHAR(700) NOT NULL DEFAULT '''' AFTER error');
CALL cicd_add_release_artifact_column('image_digest', 'ALTER TABLE project_releases ADD COLUMN image_digest VARCHAR(80) NOT NULL DEFAULT '''' AFTER image_ref');
CALL cicd_add_release_artifact_column('image_commit_sha', 'ALTER TABLE project_releases ADD COLUMN image_commit_sha VARCHAR(128) NOT NULL DEFAULT '''' AFTER image_digest');
CALL cicd_add_release_artifact_column('image_built_at', 'ALTER TABLE project_releases ADD COLUMN image_built_at DATETIME(3) NULL AFTER image_commit_sha');

DROP PROCEDURE cicd_add_release_artifact_column;
