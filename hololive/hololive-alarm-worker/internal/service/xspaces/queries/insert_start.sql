INSERT INTO x_space_starts (space_id, payload) VALUES ($1, $2)
ON CONFLICT (space_id) DO NOTHING
