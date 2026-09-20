CREATE TABLE IF NOT EXISTS project_roles (
  id VARCHAR(64) NOT NULL PRIMARY KEY,
  space_id VARCHAR(64) NOT NULL,
  `key` VARCHAR(80) NOT NULL,
  name VARCHAR(120) NOT NULL,
  description VARCHAR(255) NOT NULL DEFAULT '',
  created_by BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  UNIQUE KEY uk_project_roles_space_name (space_id, name),
  UNIQUE KEY uk_project_roles_space_key (space_id, `key`),
  KEY idx_project_roles_space (space_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS project_role_permissions (
  role_id VARCHAR(64) NOT NULL,
  permission_key VARCHAR(80) NOT NULL,
  PRIMARY KEY (role_id, permission_key),
  KEY idx_project_role_permissions_permission (permission_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS project_members (
  project_id VARCHAR(64) NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  space_id VARCHAR(64) NOT NULL,
  role_id VARCHAR(64) NOT NULL DEFAULT '',
  role_key VARCHAR(80) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (project_id, user_id),
  KEY idx_project_members_space_project (space_id, project_id),
  KEY idx_project_members_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO project_members (project_id, user_id, space_id, role_id, role_key, created_at, updated_at)
SELECT p.id,
       sm.user_id,
       p.space_id,
       CASE
         WHEN sm.role IN ('owner', 'admin') THEN 'system:project_maintainer'
         WHEN sm.role = 'developer' THEN 'system:project_developer'
         ELSE 'system:project_viewer'
       END,
       CASE
         WHEN sm.role IN ('owner', 'admin') THEN 'project_maintainer'
         WHEN sm.role = 'developer' THEN 'project_developer'
         ELSE 'project_viewer'
       END,
       CURRENT_TIMESTAMP(6),
       CURRENT_TIMESTAMP(6)
FROM projects p
JOIN space_members sm ON sm.space_id = p.space_id
ON DUPLICATE KEY UPDATE
  project_id = VALUES(project_id);
