-- Project-level release completion policy. The selected deployment target
-- triggers a merge from the release branch into the project's default branch.
DROP PROCEDURE IF EXISTS cicd_add_project_auto_merge;
DELIMITER $$
CREATE PROCEDURE cicd_add_project_auto_merge()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'projects' AND column_name = 'auto_merge_enabled'
    ) THEN
        ALTER TABLE projects ADD COLUMN auto_merge_enabled BOOLEAN NOT NULL DEFAULT FALSE AFTER default_branch;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'projects' AND column_name = 'auto_merge_target_id'
    ) THEN
        ALTER TABLE projects ADD COLUMN auto_merge_target_id VARCHAR(64) NOT NULL DEFAULT '' AFTER auto_merge_enabled;
    END IF;
END$$
DELIMITER ;
CALL cicd_add_project_auto_merge();
DROP PROCEDURE cicd_add_project_auto_merge;
