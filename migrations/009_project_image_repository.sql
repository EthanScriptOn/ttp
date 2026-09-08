-- A project may specify the image repository that its release artifacts are
-- pushed to. Empty keeps the platform-owned prefix naming. The value has no
-- tag or digest: the builder stamps a commit tag and deploys the digest.
DROP PROCEDURE IF EXISTS cicd_add_project_image_repository;
DELIMITER $$
CREATE PROCEDURE cicd_add_project_image_repository(
    IN p_column_name VARCHAR(64),
    IN p_alter_sql VARCHAR(1000)
)
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'projects' AND column_name = p_column_name
    ) THEN
        SET @cicd_project_image_sql = p_alter_sql;
        PREPARE cicd_project_image_statement FROM @cicd_project_image_sql;
        EXECUTE cicd_project_image_statement;
        DEALLOCATE PREPARE cicd_project_image_statement;
    END IF;
END$$
DELIMITER ;

CALL cicd_add_project_image_repository('image_repository', 'ALTER TABLE projects ADD COLUMN image_repository VARCHAR(500) NOT NULL DEFAULT '''' AFTER container_port');

DROP PROCEDURE cicd_add_project_image_repository;
