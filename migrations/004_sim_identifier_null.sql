-- SIM services identify themselves through sim_details (card type, number,
-- country/region, ICCID). The generic identifier column is reserved for every
-- other category, so legacy SIM values are cleared once and blocked afterwards.
UPDATE service_items SET identifier = NULL
WHERE identifier IS NOT NULL
    AND category_id IN (SELECT id FROM service_categories WHERE kind = 'sim');

CREATE TRIGGER service_items_sim_identifier_insert BEFORE INSERT ON service_items
WHEN NEW.identifier IS NOT NULL
    AND (SELECT kind FROM service_categories WHERE id = NEW.category_id) = 'sim'
BEGIN
    SELECT RAISE(ABORT, 'sim services cannot store an identifier');
END;

CREATE TRIGGER service_items_sim_identifier_update BEFORE UPDATE OF identifier, category_id ON service_items
WHEN NEW.identifier IS NOT NULL
    AND (SELECT kind FROM service_categories WHERE id = NEW.category_id) = 'sim'
BEGIN
    SELECT RAISE(ABORT, 'sim services cannot store an identifier');
END;
