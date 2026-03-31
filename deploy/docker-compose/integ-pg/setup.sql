CREATE USER integ_tests WITH PASSWORD 'integ-tests-password';
CREATE DATABASE integ_db WITH OWNER integ_tests ENCODING 'UTF8' LC_COLLATE = 'en_US.utf8' LC_CTYPE = 'en_US.utf8' ;

CREATE USER schema_manager WITH PASSWORD 'schema_admin_password' CREATEDB;
GRANT CONNECT ON DATABASE postgres TO schema_manager;
GRANT ALL PRIVILEGES ON DATABASE integ_db TO schema_manager;
ALTER DATABASE integ_db WITH is_template TRUE;
