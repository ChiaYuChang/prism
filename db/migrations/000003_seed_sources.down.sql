BEGIN;
UPDATE sources
SET deleted_at = NOW()
WHERE abbr IN ('dpp', 'kmt', 'tpp', 'cna', 'pts', 'ttv', 'yahoo')
  AND deleted_at IS NULL;
COMMIT;
