-- Seed DIRECTORY_FETCH tasks for integration test plan Phase 1.
--
-- Apply once after `task migrate:up`. Scheduler will claim these tasks
-- and dispatch TaskSignal{Kind=DIRECTORY_FETCH} to the discovery worker,
-- which then publishes PAGE_FETCH tasks per discovered candidate (PARTY
-- only) for the collector to process.
--
-- Three PARTY sources are seeded (dpp / tpp / kmt). Each source's index
-- page yields ~10 candidates, totaling ~30 articles into the fixture
-- corpus when run with --capture-dir=testdata/fixtures.
--
-- Usage:
--   psql "$PRISM_DSN" -f testdata/seed-tasks.sql

INSERT INTO public.tasks (batch_id, kind, source_type, source_abbr, url, trace_id)
VALUES
    (uuidv7(), 'DIRECTORY_FETCH', 'PARTY', 'dpp',
     'https://www.dpp.org.tw/media/00',
     'integ-test-dpp'),
    (uuidv7(), 'DIRECTORY_FETCH', 'PARTY', 'tpp',
     'https://www.tpp.org.tw/media',
     'integ-test-tpp'),
    (uuidv7(), 'DIRECTORY_FETCH', 'PARTY', 'kmt',
     'https://www.kmt.org.tw/feeds/posts/summary/-/新聞稿?start-index=1&max-results=10',
     'integ-test-kmt');
