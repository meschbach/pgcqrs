CREATE USER integ_tests WITH PASSWORD 'integ-tests-password';
CREATE DATABASE integ_db WITH OWNER integ_tests ENCODING 'UTF8' LC_COLLATE = 'en_US.utf8' LC_CTYPE = 'en_US.utf8' ;
