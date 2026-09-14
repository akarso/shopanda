-- Tag membership for cache.Cache.SetWithTags / DeleteByTag (PR-1039).
-- FK-less by design: cache rows are transient and already have their own
-- TTL-expiry path. A tag row pointing at an expired or deleted cache key is
-- harmless — DeleteByTag deletes zero cache rows for it, and DeleteExpired
-- also sweeps orphaned tag rows.
CREATE UNLOGGED TABLE cache_tags (
    tag TEXT NOT NULL,
    key TEXT NOT NULL,
    PRIMARY KEY (tag, key)
);

-- Looks up / deletes tag rows by cache key (Delete, DeleteByPrefix, DeleteExpired orphan sweep).
CREATE INDEX idx_cache_tags_key ON cache_tags (key);
