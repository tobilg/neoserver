# SQL-view authoring boundary

SQL-view authors are trusted workspace administrators, not database or host
administrators. PostGIS views accept one parsed SELECT (including read-only
CTEs). All nested statements are checked; SELECT INTO, row locking, table
sampling, and functions outside an explicit built-in/PostGIS allowlist are
rejected. Quoting, qualification and comments do not bypass function checks.
The allowlist is in `internal/datasource/postgis/sql_safety.go`; do not replace
it with a function prefix or a regex denylist.

Validation and execution use a separate pool with read-only transactions, a
10-second statement timeout, a 2-second lock timeout and a fixed
`pg_catalog,public` search path. WFS writes use the ordinary datasource pool and
still require their own publication/operation authorization. DuckDB uses its
own restricted SQL implementation; GeoParquet/vector-file SQL views are not supported.

These checks are defense in depth, **not a sandbox against the database owner**.
Operators must control schemas, views, casts, operators and functions: a SELECT
can invoke database-defined behavior indirectly. Never let SQL-view authors
create database functions or use a superuser connection in production.
Install PostGIS into an operator-owned schema (`public` for this implementation).

Example provisioning by a database administrator (substitute your database,
schema, role and secret; do not use the demo `postgres` account):

```sql
CREATE ROLE neoserver_reader LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION;
-- Set the password using your secret provisioning system or psql \password.
GRANT CONNECT ON DATABASE geodata TO neoserver_reader;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public, published TO neoserver_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA published TO neoserver_reader;
-- Run this as the owner that will create future published tables:
ALTER DEFAULT PRIVILEGES IN SCHEMA published GRANT SELECT ON TABLES TO neoserver_reader;
```

Do not grant server-file/program-execution roles, ownership or CREATE privileges.
Review existing views and function EXECUTE grants in exposed schemas. If WFS
transactions are needed, use a separately configured store/role with only the
required INSERT/UPDATE/DELETE table grants. Use TLS verification for remote
PostGIS connections. Existing SQL views using unlisted functions must be
rewritten or reviewed before upgrading; unsafe definitions are not grandfathered.

The parser is [pg_query_go](https://github.com/pganalyze/pg_query_go), which uses
PostgreSQL's parser rather than attempting to recognize SQL with regexes.
