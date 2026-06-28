-- Enforce one placement row per target position to detect concurrent replace conflicts.
CREATE UNIQUE INDEX IF NOT EXISTS idx_content_block_placements_target_position
    ON content_block_placements (target_type, target_key, position);
