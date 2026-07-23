BEGIN;

REVOKE prism_rootctl FROM prism;
REVOKE ALL ON FUNCTION prism_root_init(UUID, TEXT, TEXT, TEXT, TIMESTAMPTZ) FROM prism_rootctl;
REVOKE ALL ON FUNCTION prism_root_check(TEXT, TEXT) FROM prism_rootctl;
REVOKE ALL ON FUNCTION prism_root_create_admin(TEXT, TEXT, UUID, TEXT, TEXT, TEXT, TIMESTAMPTZ) FROM prism_rootctl;
REVOKE ALL ON FUNCTION prism_root_revoke_all(TEXT, TEXT) FROM prism_rootctl;
DROP FUNCTION IF EXISTS prism_root_revoke_all(TEXT, TEXT);
DROP FUNCTION IF EXISTS prism_root_create_admin(TEXT, TEXT, UUID, TEXT, TEXT, TEXT, TIMESTAMPTZ);
DROP FUNCTION IF EXISTS prism_root_check(TEXT, TEXT);
DROP FUNCTION IF EXISTS prism_root_init(UUID, TEXT, TEXT, TEXT, TIMESTAMPTZ);

COMMIT;
