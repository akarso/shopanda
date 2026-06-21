package config

// CorePostgresSearchEnabled reports whether the postgres search core plugin should load.
// Explicit plugins.core.postgres_search overrides search.engine inference.
func (c *Config) CorePostgresSearchEnabled() bool {
	if c.Plugins.Core.PostgresSearch != nil {
		return *c.Plugins.Core.PostgresSearch
	}
	engine := c.Search.Engine
	return engine == "" || engine == "postgres"
}

// CoreMeilisearchSearchEnabled reports whether the Meilisearch search core plugin should load.
func (c *Config) CoreMeilisearchSearchEnabled() bool {
	return c.Search.Engine == "meilisearch"
}

// CoreLocalStorageEnabled reports whether the local filesystem storage core plugin should load.
func (c *Config) CoreLocalStorageEnabled() bool {
	storage := c.Media.Storage
	return storage == "" || storage == "local"
}

// CoreS3StorageEnabled reports whether the S3 storage core plugin should load.
func (c *Config) CoreS3StorageEnabled() bool {
	return c.Media.Storage == "s3"
}

// CorePostgresCacheEnabled reports whether the postgres cache core plugin should load.
func (c *Config) CorePostgresCacheEnabled() bool {
	if c.Plugins.Core.PostgresCache != nil {
		return *c.Plugins.Core.PostgresCache
	}
	return c.Cache.Driver == "" || c.Cache.Driver == "postgres"
}

// CorePostgresQueueEnabled reports whether the postgres queue core plugin should load.
func (c *Config) CorePostgresQueueEnabled() bool {
	if c.Plugins.Core.PostgresQueue != nil {
		return *c.Plugins.Core.PostgresQueue
	}
	driver := c.Queue.Driver
	return driver == "" || driver == "postgres"
}
