-- Store the gateway routing rule separately from the assignment mode.
-- Existing user_id experiments remain valid and default to $.user_id.
DROP PROCEDURE IF EXISTS cicd_add_ab_routing_rule;
DELIMITER $$
CREATE PROCEDURE cicd_add_ab_routing_rule()
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = DATABASE()
          AND table_name = 'ab_experiments'
          AND column_name = 'routing_rule'
    ) THEN
        ALTER TABLE ab_experiments
            ADD COLUMN routing_rule JSON NULL AFTER assignment;
    END IF;
END$$
DELIMITER ;
CALL cicd_add_ab_routing_rule();
DROP PROCEDURE cicd_add_ab_routing_rule;

UPDATE ab_experiments
SET routing_rule = JSON_OBJECT(
    'source', 'json_body',
    'path', '$.user_id',
    'missing_behavior', 'stable',
    'algorithm', 'consistent_hash'
)
WHERE assignment = 'user_id' AND routing_rule IS NULL;
