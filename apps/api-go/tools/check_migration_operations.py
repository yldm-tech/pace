"""Check a ported RunPython against the Python it was ported from, by running both and diffing the databases.

Reading a RunPython and writing the SQL it amounts to is guesswork, and the places it goes wrong are not the places it looks difficult: whether a queryset's exclude() takes the null rows with it, what Django's JSONField does to an empty object, which of two values a bulk_update writes when the rows disagree. None of that is worth arguing about when both sides can simply be run.

For each migration named on the command line this builds two databases, migrates both to the migration *before* it, seeds them identically from testdata/operations/<app>.<name>.sql, and then lets Django apply the migration to one and the Go engine apply it to the other. Every table is dumped from both and compared. A difference is printed as rows, not as a schema diff, because what is being checked is what happened to the data.

Run from apps/api:

    python ../api-go/tools/check_migration_operations.py db.0035_auto_20230704_2225 ...

With no arguments it checks every migration that has a seed file.
"""

import os
import subprocess
import sys
from urllib.parse import urlsplit, urlunsplit

import django

os.environ.setdefault("DJANGO_SETTINGS_MODULE", "plane.settings.production")

HERE = os.path.dirname(os.path.abspath(__file__))
API_GO = os.path.dirname(HERE)
SEEDS = os.path.join(API_GO, "internal", "migrate", "testdata", "operations")

DJANGO_DB = "opcheck_django"
GO_DB = "opcheck_go"


def base_url():
    """The configured database's server, with the database name left to the caller."""
    url = os.environ.get("DATABASE_URL")
    if not url:
        raise SystemExit("set DATABASE_URL to the server the two scratch databases should be made on")
    return urlsplit(url)


def url_for(name):
    parts = base_url()
    return urlunsplit((parts.scheme, parts.netloc, "/" + name, parts.query, parts.fragment))


def psql(database, *arguments):
    result = subprocess.run(["psql", url_for(database), "-v", "ON_ERROR_STOP=1", *arguments],
                            capture_output=True, text=True)
    if result.returncode != 0:
        # Raised rather than returned, and with the server's own message, because the alternative is a traceback naming the psql invocation and not the line of SQL that was wrong.
        raise SystemExit(f"psql against {database} failed:\n{result.stderr.strip()}")
    return result


def recreate(database):
    subprocess.run(["psql", url_for("postgres"), "-v", "ON_ERROR_STOP=1",
                    "-c", f'DROP DATABASE IF EXISTS "{database}"',
                    "-c", f'CREATE DATABASE "{database}"'], check=True, capture_output=True, text=True)


def previous(app, name):
    """The migration Django applies immediately before this one, which is the state the operation acts on."""
    from django.db import connection
    from django.db.migrations.loader import MigrationLoader

    loader = MigrationLoader(connection, ignore_no_migrations=True)
    ordered = []
    seen = set()
    for target in sorted(loader.graph.leaf_nodes()):
        for node in loader.graph.forwards_plan(target):
            if node not in seen:
                seen.add(node)
                ordered.append(node)
    index = ordered.index((app, name))
    if index == 0:
        raise SystemExit(f"{app}.{name} is the first migration, so there is no state before it to seed")
    return ordered[index - 1]


def migrate_with_django(database, app, name):
    environment = dict(os.environ, DATABASE_URL=url_for(database))
    subprocess.run([sys.executable, "manage.py", "migrate", app, name, "--noinput", "-v", "0"],
                   check=True, env=environment, capture_output=True, text=True)


def migrate_with_go(database, app, name):
    environment = dict(os.environ, DATABASE_URL=url_for(database))
    result = subprocess.run(["go", "run", "./cmd/manage", "migrate", app, name],
                            cwd=API_GO, env=environment, capture_output=True, text=True)
    if result.returncode != 0:
        raise SystemExit(f"the Go engine refused {app}.{name}:\n{result.stdout}{result.stderr}")


def random_columns(seed):
    """Columns the operation fills with random values, named in the seed file as `-- RANDOM: table.column`.

    Four of these operations exist to scatter rows into an arbitrary order — sort_order, the cycle and module orderings — and call random.randint to do it. Two runs of the same Python disagree with each other, so comparing the values would only ever prove that random is random. What is compared instead is everything else, and the column itself is checked for being filled in at all.
    """
    found = []
    for line in open(seed):
        marker = "-- RANDOM:"
        if line.startswith(marker):
            for entry in line[len(marker):].split(","):
                table, _, column = entry.strip().partition(".")
                if table and column:
                    found.append((table, column))
    return found


def assert_filled(database, columns):
    """Every row got a value for each random column.

    A row the operation skipped is a real difference and has to fail here, since the values themselves are about to be left out of the comparison and nothing else would notice.
    """
    for table, column in columns:
        missing = psql(database, "-qtA", "-c", f'SELECT count(*) FROM "{table}" WHERE "{column}" IS NULL').stdout.strip()
        if missing != "0":
            raise SystemExit(f"{database}: {missing} row(s) of {table}.{column} were left null by an operation that should have filled every one")


def dump_rows(database, skip):
    """Every row of every table, as text, sorted.

    The sort is done here rather than in SQL because most of these tables begin with created_at and no single column orders them, and what is being compared is the set of rows rather than the order the planner happened to return them in.

    The ledger and the two tables post_migrate writes are left out: the ledger's applied timestamps differ by construction, and the other two are not written by a migration at all.
    """
    listing = psql(database, "-qtA", "-c", """
        SELECT tablename FROM pg_tables
         WHERE schemaname = 'public'
           AND tablename NOT IN ('django_migrations', 'django_content_type', 'auth_permission')
         ORDER BY tablename
    """).stdout.split()
    dumped = {}
    for table in listing:
        skipped = {column for skipped_table, column in skip if skipped_table == table}
        if skipped:
            # Named columns rather than *, so a random value can be left out of the comparison. Dropping it here rather than nulling it in the table matters: several of these columns are NOT NULL by the time the migration finishes.
            columns = psql(database, "-qtA", "-c", f"""
                SELECT column_name FROM information_schema.columns
                 WHERE table_schema = 'public' AND table_name = '{table}'
                 ORDER BY ordinal_position
            """).stdout.split()
            kept = ", ".join(f'"{column}"' for column in columns if column not in skipped)
            selection = f'SELECT {kept} FROM "{table}"'
        else:
            selection = f'SELECT * FROM "{table}"'
        copied = psql(database, "-qtA", "-c", f'COPY ({selection}) TO STDOUT').stdout
        dumped[table] = sorted(copied.splitlines())
    return dumped


def check(app, name):
    seed = os.path.join(SEEDS, f"{app}.{name}.sql")
    if not os.path.exists(seed):
        raise SystemExit(f"no seed for {app}.{name}; write {seed} first")
    before_app, before_name = previous(app, name)

    for database in (DJANGO_DB, GO_DB):
        recreate(database)
        migrate_with_django(database, before_app, before_name)
        psql(database, "-q", "-f", seed)

    migrate_with_django(DJANGO_DB, app, name)
    migrate_with_go(GO_DB, app, name)

    scattered = random_columns(seed)
    for database in (DJANGO_DB, GO_DB):
        assert_filled(database, scattered)

    django_rows = dump_rows(DJANGO_DB, scattered)
    go_rows = dump_rows(GO_DB, scattered)

    differences = []
    for table in sorted(set(django_rows) | set(go_rows)):
        if django_rows.get(table) != go_rows.get(table):
            differences.append(table)
    if not differences:
        print(f"  {app}.{name}: the two databases agree")
        return True

    print(f"  {app}.{name}: {len(differences)} table(s) differ")
    for table in differences:
        print(f"    --- {table} ---")
        mine = django_rows.get(table, [])
        theirs = go_rows.get(table, [])
        for line in mine:
            if line not in theirs:
                print(f"    django only: {line}")
        for line in theirs:
            if line not in mine:
                print(f"    go only    : {line}")
    return False


def main():
    django.setup()
    names = sys.argv[1:]
    if not names:
        names = sorted(f[:-4] for f in os.listdir(SEEDS) if f.endswith(".sql"))
    failed = []
    for full in names:
        app, _, name = full.partition(".")
        if not check(app, name):
            failed.append(full)
    if failed:
        raise SystemExit(f"{len(failed)} operation(s) do not match Django: {', '.join(failed)}")
    print(f"{len(names)} migration(s) checked, all matching")


if __name__ == "__main__":
    main()
