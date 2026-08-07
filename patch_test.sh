#!/bin/bash
FILE="internal/repo/pg/analysis_runs_integration_test.go"

# 1. sourceAbbr length: test_source_ (12 chars) + uuid[:8] = 20 chars. Max is 16 chars.
# Let's change test_source_ to test_
sed -i 's/"test_source_"/"test_"/g' $FILE

# 2. Table renames: user_fetches -> fetches, user_fetch_items -> fetch_items
sed -i 's/user_fetches/fetches/g' $FILE
sed -i 's/user_fetch_items/fetch_items/g' $FILE

# 3. IGNORE -> IGNORE_FAILED
sed -i 's/"IGNORE"/"IGNORE_FAILED"/g' $FILE
sed -i "s/'IGNORE'/'IGNORE_FAILED'/g" $FILE
sed -i "s/'FETCHING', 'IGNORE_FAILED'/'FAILED', 'IGNORE_FAILED'/g" $FILE # Revert back if it caught the line, wait no, my previous replace was wrong.
# Wait, let me just specifically replace the analysis_runs insert status
sed -i "s/VALUES (\$1, \$2, \$3, 'FETCHING', 'IGNORE_FAILED', '{}')/VALUES (\$1, \$2, \$3, 'FETCHING', 'IGNORE_FAILED', '{}')/g" $FILE

# 4. Add trace_id to candidates
sed -i "s/url, ingestion_method)/url, ingestion_method, trace_id)/g" $FILE
sed -i "s/'DIRECTORY')/'DIRECTORY', 'test-trace')/g" $FILE

sed -i "s/fingerprint, url)/fingerprint, url, trace_id)/g" $FILE
sed -i "s/\"https:\/\/example.com\/1\")/\"https:\/\/example.com\/1\", 'test-trace')/g" $FILE
sed -i "s/\"https:\/\/example.com\/2\")/\"https:\/\/example.com\/2\", 'test-trace')/g" $FILE
sed -i "s/\"https:\/\/example.com\/soft-delete\")/\"https:\/\/example.com\/soft-delete\", 'test-trace')/g" $FILE

# 5. Add trace_id to tasks
sed -i "s/url, status)/url, status, trace_id)/g" $FILE
sed -i "s/'PENDING')/'PENDING', 'test-trace')/g" $FILE
sed -i "s/'COMPLETED')/'COMPLETED', 'test-trace')/g" $FILE

# 6. Add type to contents
sed -i "s/content, fetched_at)/content, fetched_at, type)/g" $FILE
sed -i "s/<html>body<\/html>', \$3)/<html>body<\/html>', \$3, 'ARTICLE')/g" $FILE

# 7. Remove n_subtasks, max_subtasks from batches
sed -i "s/, n_subtasks, max_subtasks//g" $FILE
sed -i "s/, 0, 10//g" $FILE

