ALTER TABLE products ADD COLUMN search_vector tsvector;

UPDATE products SET search_vector = to_tsvector('english', coalesce(name, '') || ' ' || coalesce(description, ''));

CREATE INDEX idx_products_search_vector ON products USING GIN (search_vector);

CREATE OR REPLACE FUNCTION products_search_vector_update() RETURNS trigger AS $$
BEGIN
    NEW.search_vector := to_tsvector('english', coalesce(NEW.name, '') || ' ' || coalesce(NEW.description, ''));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER products_search_vector_trigger
    BEFORE INSERT OR UPDATE OF name, description ON products
    FOR EACH ROW EXECUTE FUNCTION products_search_vector_update();
