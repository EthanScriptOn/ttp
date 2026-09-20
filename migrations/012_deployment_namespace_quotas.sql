-- Resource budgets belong to the generated namespace, not to an individual
-- project. One space/cluster/environment tuple therefore has one policy.
CREATE TABLE IF NOT EXISTS deployment_namespace_quotas (
    space_id VARCHAR(64) NOT NULL,
    cluster_id VARCHAR(64) NOT NULL,
    environment VARCHAR(64) NOT NULL,
    namespace VARCHAR(120) NOT NULL,
    cpu_request VARCHAR(32) NOT NULL,
    cpu_limit VARCHAR(32) NOT NULL,
    memory_request VARCHAR(32) NOT NULL,
    memory_limit VARCHAR(32) NOT NULL,
    ephemeral_storage_request VARCHAR(32) NOT NULL,
    ephemeral_storage_limit VARCHAR(32) NOT NULL,
    storage VARCHAR(32) NOT NULL,
    pods INT UNSIGNED NOT NULL,
    persistent_volume_claims INT UNSIGNED NOT NULL,
    default_cpu_request VARCHAR(32) NOT NULL,
    default_cpu_limit VARCHAR(32) NOT NULL,
    default_memory_request VARCHAR(32) NOT NULL,
    default_memory_limit VARCHAR(32) NOT NULL,
    default_ephemeral_storage_request VARCHAR(32) NOT NULL,
    default_ephemeral_storage_limit VARCHAR(32) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (space_id, cluster_id, environment),
    KEY idx_namespace_quotas_cluster (space_id, cluster_id, namespace),
    CONSTRAINT fk_namespace_quotas_space FOREIGN KEY (space_id)
        REFERENCES spaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_namespace_quotas_cluster_same_space FOREIGN KEY (space_id, cluster_id)
        REFERENCES clusters (space_id, id) ON DELETE CASCADE,
    CONSTRAINT chk_namespace_quotas_pods CHECK (pods > 0),
    CONSTRAINT chk_namespace_quotas_pvcs CHECK (persistent_volume_claims > 0)
) ENGINE = InnoDB;
