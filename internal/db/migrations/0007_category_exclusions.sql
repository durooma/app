ALTER TABLE categories
    ADD COLUMN exclude_from_calculations BOOLEAN NOT NULL DEFAULT false;

-- Transfers remain available for categorization but do not represent income or
-- spending. Apply this default once; subsequent category edits are respected.
UPDATE categories SET exclude_from_calculations = true WHERE lower(name) = 'transfer';
INSERT INTO categories (name, description, sort_order, exclude_from_calculations)
SELECT 'Transfer', 'Transfers between accounts; neither income nor spending', 910, true
WHERE NOT EXISTS (SELECT 1 FROM categories WHERE lower(name) = 'transfer');
