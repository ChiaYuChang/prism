BEGIN;

DELETE FROM sources WHERE abbr IN ('dpp', 'kmt', 'tpp', 'cna', 'pts', 'ttv', 'yahoo');

COMMIT;
