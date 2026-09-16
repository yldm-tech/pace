"""Print a seed skeleton for check_migration_operations.py, filled in far enough to insert.

Writing these by hand means looking up which of a table's forty columns are NOT NULL without a default, and Plane's tables inherit a dozen such columns from a common base. That is not work worth doing by hand fifty-seven times. This builds a database at the state before the migration, reads the columns that must be supplied, and prints an INSERT per requested row with a literal of the right type in each.

What it cannot do is decide what the values should *mean*. The point of a seed is to hold the rows the operation will disagree about — the null one, the empty one, the one that is already right — so the output is a starting point to edit, not an answer.

Run from apps/api:

    python ../api-go/tools/seed_migration_operation.py db.0035_auto_20230704_2225 workspaces users
"""

import os
import subprocess
import sys
from urllib.parse import urlsplit, urlunsplit

import django

os.environ.setdefault("DJANGO_SETTINGS_MODULE", "plane.settings.production")

SCRATCH = "seed_skeleton"
# One fixed instant for every timestamp. The two databases a seed is loaded into are seeded by separate runs, so a now() would differ between them and every row would mismatch for a reason that has nothing to do with the operation under test.
INSTANT = "'2024-01-01 00:00:00+00'"


def url_for(name):
    parts = urlsplit(os.environ["DATABASE_URL"])
    return urlunsplit((parts.scheme, parts.netloc, "/" + name, parts.query, parts.fragment))


def psql(database, *arguments):
    return subprocess.run(["psql", url_for(database), "-v", "ON_ERROR_STOP=1", *arguments],
                          check=True, capture_output=True, text=True)


def literal(data_type, column):
    """A value of the right type, chosen to be obviously placeholder rather than plausible."""
    if data_type in ("timestamp with time zone", "timestamp without time zone", "date"):
        return INSTANT
    if data_type == "uuid":
        return "'00000000-0000-4000-8000-000000000000'"
    if data_type == "boolean":
        return "false"
    if data_type in ("integer", "bigint", "smallint", "numeric", "double precision", "real"):
        return "0"
    if data_type == "jsonb":
        return "'{}'"
    if data_type == "ARRAY":
        return "'{}'"
    return f"'{column}'"


def required_columns(database, table):
    rows = psql(database, "-qtAF|", "-c", f"""
        SELECT column_name, data_type FROM information_schema.columns
         WHERE table_schema = 'public' AND table_name = '{table}'
           AND is_nullable = 'NO' AND column_default IS NULL
         ORDER BY ordinal_position
    """).stdout.strip().splitlines()
    if not rows:
        raise SystemExit(f"{table} has no columns that must be supplied, or does not exist at this point")
    return [line.split("|") for line in rows]


# The cast every seed needs before it can insert anything of its own: someone to own things, a workspace and two projects. Their ids are fixed so a seed can refer to them, and everything else is placeholder.
PRELUDE = [
    ("users", "00000000-0000-4000-8000-000000000001", {}),
    ("users", "00000000-0000-4000-8000-000000000002", {"username": "'second'", "email": "'second@example.test'", "display_name": "'second'"}),
    ("workspaces", "00000000-0000-4000-8000-000000000101", {"owner_id": "'00000000-0000-4000-8000-000000000001'", "slug": "'acme'"}),
    ("projects", "00000000-0000-4000-8000-000000000201", {"workspace_id": "'00000000-0000-4000-8000-000000000101'", "identifier": "'APO'", "name": "'Apollo'"}),
    ("projects", "00000000-0000-4000-8000-000000000202", {"workspace_id": "'00000000-0000-4000-8000-000000000101'", "identifier": "'GEM'", "name": "'Gemini'"}),
]


def prelude(database):
    """The standard cast, with whatever columns the schema at this point insists on.

    Which columns those are moves about: a user carried is_onboarded and a theme until db.0065 moved them to a profile, and a project grew logo_props at db.0061. Writing the prelude out by hand per seed means finding that out one failed insert at a time, so it is generated instead.
    """
    lines = []
    for table, row_id, overrides in PRELUDE:
        columns = required_columns(database, table)
        names = ", ".join(column for column, _ in columns)
        values = []
        for column, data_type in columns:
            if column in overrides:
                values.append(overrides[column])
            elif column == "id":
                values.append(f"'{row_id}'")
            else:
                values.append(literal(data_type, column))
        lines.append(f"INSERT INTO {table} ({names})")
        lines.append(f"VALUES ({', '.join(values)});")
        lines.append("")
    return "\n".join(lines)


def main():
    django.setup()
    if len(sys.argv) < 3:
        raise SystemExit(f"usage: {sys.argv[0]} <app.migration> <table> [table ...]")
    full = sys.argv[1]
    app, _, name = full.partition(".")
    tables = [table for table in sys.argv[2:] if table != "--prelude"]
    want_prelude = "--prelude" in sys.argv[2:]

    from django.db import connection
    from django.db.migrations.loader import MigrationLoader

    loader = MigrationLoader(connection, ignore_no_migrations=True)
    ordered, seen = [], set()
    for target in sorted(loader.graph.leaf_nodes()):
        for node in loader.graph.forwards_plan(target):
            if node not in seen:
                seen.add(node)
                ordered.append(node)
    before_app, before_name = ordered[ordered.index((app, name)) - 1]

    subprocess.run(["psql", url_for("postgres"), "-v", "ON_ERROR_STOP=1",
                    "-c", f'DROP DATABASE IF EXISTS "{SCRATCH}"',
                    "-c", f'CREATE DATABASE "{SCRATCH}"'], check=True, capture_output=True, text=True)
    subprocess.run([sys.executable, "manage.py", "migrate", before_app, before_name, "--noinput", "-v", "0"],
                   check=True, env=dict(os.environ, DATABASE_URL=url_for(SCRATCH)), capture_output=True, text=True)

    print(f"-- Rows for {full} to act on. Skeleton from apps/api-go/tools/seed_migration_operation.py; edit the values that matter.")
    print(f"-- The schema here is the one {before_app}.{before_name} leaves behind.")
    if want_prelude:
        print()
        print(prelude(SCRATCH), end="")
    for table in tables:
        columns = required_columns(SCRATCH, table)
        names = ", ".join(column for column, _ in columns)
        values = ", ".join(literal(data_type, column) for column, data_type in columns)
        print()
        print(f"INSERT INTO {table} ({names})")
        print(f"VALUES ({values});")


if __name__ == "__main__":
    main()
