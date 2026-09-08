-- Each project may use its own Git machine account. The token is encrypted by
-- the TTP API before it reaches this table and is never returned to clients.
CREATE TABLE IF NOT EXISTS project_git_credentials (
    project_id VARCHAR(64) NOT NULL,
    space_id VARCHAR(64) NOT NULL,
    provider VARCHAR(32) NOT NULL,
    username VARCHAR(120) NOT NULL,
    token_ciphertext LONGTEXT NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (project_id),
    UNIQUE KEY uk_project_git_credentials_space_id (space_id, project_id),
    CONSTRAINT fk_project_git_credentials_project_same_space
        FOREIGN KEY (space_id, project_id) REFERENCES projects (space_id, id) ON DELETE CASCADE
) ENGINE = InnoDB;
