CREATE TABLE categorization_state (
    id BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    category_revision BIGINT NOT NULL DEFAULT 0,
    run_revision BIGINT
);
-- Existing assignments establish the initial baseline when upgrading.
INSERT INTO categorization_state (id, run_revision)
VALUES (true, CASE WHEN EXISTS (SELECT 1 FROM transactions WHERE category_id IS NOT NULL) THEN 0 END);

CREATE FUNCTION track_category_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW IS NOT DISTINCT FROM OLD THEN RETURN NULL; END IF;
    END IF;
    UPDATE categorization_state SET category_revision = category_revision + 1 WHERE id;
    RETURN NULL;
END;
$$;
CREATE TRIGGER categories_revision AFTER INSERT OR UPDATE OR DELETE ON categories
FOR EACH ROW EXECUTE FUNCTION track_category_revision();
