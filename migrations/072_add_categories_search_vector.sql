ALTER TABLE categories ADD COLUMN search_vector tsvector;

UPDATE categories SET search_vector = to_tsvector('english', coalesce(name, '') || ' ' || coalesce(slug, ''));

CREATE INDEX idx_categories_search_vector ON categories USING GIN (search_vector);

CREATE OR REPLACE FUNCTION categories_search_vector_update() RETURNS trigger AS $$
BEGIN
    NEW.search_vector := to_tsvector('english', coalesce(NEW.name, '') || ' ' || coalesce(NEW.slug, ''));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER categories_search_vector_trigger
    BEFORE INSERT OR UPDATE OF name, slug ON categories
    FOR EACH ROW EXECUTE FUNCTION categories_search_vector_update();
