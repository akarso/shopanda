-- Support operator purge by field_code (extension-field retire --purge-values).
CREATE INDEX IF NOT EXISTS extension_values_field_code_idx ON extension_values (field_code);
