-- Release flows keep the mutable set of participating branches separate from
-- immutable project_releases snapshots. Each release stores a JSON snapshot so
-- historical records remain auditable after a branch is removed.
DROP PROCEDURE IF EXISTS cicd_add_release_flow_columns;
DELIMITER $$
CREATE PROCEDURE cicd_add_release_flow_columns()
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'project_releases' AND column_name = 'flow_id') THEN
        ALTER TABLE project_releases ADD COLUMN flow_id VARCHAR(64) NOT NULL DEFAULT '' AFTER source_branch;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'project_releases' AND column_name = 'flow_version') THEN
        ALTER TABLE project_releases ADD COLUMN flow_version INT UNSIGNED NOT NULL DEFAULT 0 AFTER flow_id;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'project_releases' AND column_name = 'base_branch') THEN
        ALTER TABLE project_releases ADD COLUMN base_branch VARCHAR(120) NOT NULL DEFAULT '' AFTER flow_version;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'project_releases' AND column_name = 'flow_participants') THEN
        ALTER TABLE project_releases ADD COLUMN flow_participants JSON NULL AFTER base_branch;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'project_releases' AND column_name = 'parent_release_id') THEN
        ALTER TABLE project_releases ADD COLUMN parent_release_id VARCHAR(64) NOT NULL DEFAULT '' AFTER flow_participants;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'project_releases' AND column_name = 'replaces_release_id') THEN
        ALTER TABLE project_releases ADD COLUMN replaces_release_id VARCHAR(64) NOT NULL DEFAULT '' AFTER parent_release_id;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'project_releases' AND column_name = 'replaced_by_release_id') THEN
        ALTER TABLE project_releases ADD COLUMN replaced_by_release_id VARCHAR(64) NOT NULL DEFAULT '' AFTER replaces_release_id;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'project_releases' AND column_name = 'replaced_at') THEN
        ALTER TABLE project_releases ADD COLUMN replaced_at DATETIME(3) NULL AFTER replaced_by_release_id;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'project_releases' AND column_name = 'replacement_state') THEN
        ALTER TABLE project_releases ADD COLUMN replacement_state VARCHAR(16) NOT NULL DEFAULT '' AFTER replaced_at;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'project_releases' AND column_name = 'removed_branch') THEN
        ALTER TABLE project_releases ADD COLUMN removed_branch VARCHAR(120) NOT NULL DEFAULT '' AFTER replacement_state;
    END IF;
END$$
DELIMITER ;
CALL cicd_add_release_flow_columns();
DROP PROCEDURE cicd_add_release_flow_columns;
