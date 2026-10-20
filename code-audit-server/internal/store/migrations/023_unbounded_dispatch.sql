-- Zero represents an explicitly negotiated unbounded dispatcher, not no capacity.
-- Existing positive values remain readable as legacy fixed worker counts.
ALTER TABLE execution_agents DROP CONSTRAINT execution_agents_slots_check;
ALTER TABLE execution_agents ADD CONSTRAINT execution_agents_slots_check CHECK(slots BETWEEN 0 AND 64);
