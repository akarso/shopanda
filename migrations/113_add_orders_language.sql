-- Shopper language (BCP 47 primary tag) resolved at checkout for email /
-- order.line.display. NULL on legacy rows → fall back to store language.
ALTER TABLE orders
    ADD COLUMN language TEXT
        CHECK (language IS NULL OR language ~ '^[a-z]{2,3}$');
