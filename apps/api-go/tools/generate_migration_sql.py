"""Record the SQL every migration the Django app ships actually runs, so the Go side can run the same statements.

Django is the only authority on what its own migrations do to a database. Checking its output in makes the schema half of the port faithful by construction rather than by transcription, and CI regenerates it so a new migration cannot be forgotten.

The obvious way to get that output is sqlmigrate, and it is not good enough. sqlmigrate renders a migration without executing it, so an operation that asks the database for the name of a constraint an earlier operation in the same migration was supposed to have created finds nothing there: db.0074 drops a unique_together, alters the field and puts the unique_together back, and rendering it raises rather than printing. What is recorded here instead is the statements a real migrate executes, captured through connection.execute_wrapper while the migrations are applied one at a time.

RunPython is the exception, and it is excluded rather than recorded. Its statements depend on the rows that happen to be in the database, and the database this runs against is empty, so what it emits here says nothing about what it would emit against a real one. Those operations are ported to Go by hand; this script's job is to say which ones exist and what they are called, so the Go registry can be checked against the list.

Run from apps/api against an empty database:

    python ../api-go/tools/generate_migration_sql.py ../api-go/internal/migrate
"""

import os
import sys

import django

os.environ.setdefault("DJANGO_SETTINGS_MODULE", "plane.settings.production")
django.setup()

from django.core.management import call_command
from django.db import connection
from django.db.migrations.loader import MigrationLoader
from django.db.migrations.operations.special import RunPython, RunSQL

# Tables whose *rows* are not a migration's to write. Their tables still are: something has to create them, and the migration that does is an ordinary one.
#
# django_migrations is Django's ledger, and the Go side keeps the same one and writes its own row, so recording Django's INSERT would write it twice. The other two are filled in by post_migrate handlers rather than by operations, their inserts are the only statements here carrying bound parameters, and their end state is written out separately by write_signal_rows.
NOT_OURS = ("django_migrations", "django_content_type", "auth_permission")


def writes_rows_we_do_not_own(sql):
    """Whether this statement writes rows into a table whose contents are handled elsewhere."""
    first = sql.strip().split(None, 1)
    if not first or first[0].upper() not in ("INSERT", "UPDATE", "DELETE"):
        return False
    return any(table in sql for table in NOT_OURS)


def plan(loader):
    """Every migration in the order Django applies it, which is the graph's order and not the filename's."""
    seen = set()
    ordered = []
    for target in sorted(loader.graph.leaf_nodes()):
        for node in loader.graph.forwards_plan(target):
            if node not in seen:
                seen.add(node)
                ordered.append(node)
    return ordered


def python_operations(migration):
    """The operations in this migration that carry code rather than schema changes, named the way the Go registry names them.

    A RunSQL is listed too. Its statements are recorded like any other, so the Go side needs nothing for it, but knowing one was there is what stops a reader assuming every entry in this column has to be ported.
    """
    names = []
    for operation in migration.operations:
        if isinstance(operation, RunSQL):
            names.append("RunSQL")
        elif isinstance(operation, RunPython):
            names.append(getattr(operation.code, "__name__", "?"))
    return names


class Recorder:
    """Collects the statements one migration runs, minus the ones that are not the Go side's to run."""

    def __init__(self):
        # Steps in the order Django ran them: ("sql", statement) or ("run", function name).
        self.steps = []
        self.ledger_ddl = None
        self.inside_run_python = False

    def __call__(self, execute, sql, params, many, context):
        if not self.inside_run_python:
            # The ledger's own table is created by Django's MigrationRecorder before the first migration runs, so it belongs to no migration and has to be kept separately or the Go side has nowhere to write its first row.
            if sql.lstrip().upper().startswith("CREATE TABLE") and "django_migrations" in sql:
                self.ledger_ddl = sql
            elif changes_something(sql, params) and not writes_rows_we_do_not_own(sql):
                self.steps.append(("sql", as_literal_sql(sql, params)))
        return execute(sql, params, many, context)


def changes_something(sql, params):
    """Whether this statement is one the Go side has to replay.

    Most of what a migration runs is Django reading the database rather than writing to it: the schema editor asks pg_class and pg_constraint what an index it is about to drop is actually called, because Django does not know the name it generated years ago. Those reads are how Django decides what DDL to emit, and replaying them would do nothing.

    The rule is about reads, not about first words, and that distinction matters. Django drops a foreign key with a compound statement that begins SET CONSTRAINTS and goes on to ALTER TABLE ... DROP CONSTRAINT; skipping anything starting with SET threw the drop away and left a migration trying to add a constraint that was still there.
    """
    statement = sql.strip().rstrip(";")
    if not statement:
        return False
    # Every introspection query is parameterised and nothing that changes the schema is, so this alone catches the reads. It is kept as a separate check because a parameterised statement could not be replayed as text anyway.
    if params:
        return False
    first = statement.split(None, 1)[0].upper()
    if first in ("SELECT", "WITH", "SHOW"):
        return False
    # Transaction control on its own. Inside a compound statement it is part of doing the work, so only a statement that is nothing else is skipped.
    if ";" not in statement and first in ("BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE"):
        return False
    return True


def as_literal_sql(sql, params):
    """The statement as the Go side will replay it, which is as text rather than as a prepared statement.

    Schema operations are built by Django's schema editor as literal SQL — identifiers, types and defaults are interpolated rather than bound — so nothing that survives changes_something carries parameters. Anything that does is marked instead of guessed at, and the Go loader refuses a file carrying the marker rather than replaying it wrongly.
    """
    if not params:
        return sql
    return "-- UNRENDERABLE PARAMETERS\n" + sql


def record(app, name, migration):
    """Apply one migration, returning its steps in the order Django ran them.

    The order is the point. Django runs a migration's operations one after another, so a RunPython sits *between* schema changes rather than after them: db.0035 adds organization_size, fills it in from company_size, and a later migration drops company_size. Replaying all of a migration's SQL and then its code would have read a column that was no longer there — which is exactly what happened before this recorded where the code goes.

    Each operation is wrapped so the statements it runs are attributed to it, and a RunPython contributes a marker instead of statements. The operation objects are class attributes of the migration module and so survive the loader rebuilding the Migration, which is what makes wrapping them here reach the ones Django is about to call.
    """
    recorder = Recorder()
    unwrap = []

    for operation in migration.operations:
        if isinstance(operation, RunPython):
            def make_marker(operation):
                original = operation.database_forwards

                def traced(*args, **kwargs):
                    recorder.steps.append(("run", getattr(operation.code, "__name__", "?")))
                    recorder.inside_run_python = True
                    try:
                        return original(*args, **kwargs)
                    finally:
                        recorder.inside_run_python = False
                return original, traced

            original, traced = make_marker(operation)
            operation.database_forwards = traced
            unwrap.append((operation, original))

    try:
        with connection.execute_wrapper(recorder):
            call_command("migrate", app, name, verbosity=0, interactive=False)
    finally:
        for operation, original in unwrap:
            operation.database_forwards = original
    return recorder.steps, recorder.ledger_ddl


def write_signal_rows(root):
    """The rows migrate leaves behind that no migration wrote.

    django.contrib.contenttypes and django.contrib.auth hang handlers off post_migrate that fill django_content_type and auth_permission in as models appear. They are not operations, they carry bound parameters, and a database Django built has them while one built only from the recorded statements would not. Nothing in Plane reads either table — there is no GenericForeignKey in the app and its permission classes are DRF's, not Django's — but leaving them out would mean a Go-built database and a Django-built one differ, which would cost the comparison its value.

    The final contents are written out rather than the incremental inserts, because the end state is what has to match.
    """
    with connection.cursor() as cursor:
        cursor.execute('SELECT id, app_label, model FROM django_content_type ORDER BY id')
        content_types = cursor.fetchall()
        cursor.execute('SELECT id, name, content_type_id, codename FROM auth_permission ORDER BY id')
        permissions = cursor.fetchall()

    with open(os.path.join(root, "signal_rows.sql"), "w") as handle:
        handle.write("-- The rows post_migrate leaves behind, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.\n")
        for row_id, app_label, model in content_types:
            handle.write(f"INSERT INTO \"django_content_type\" (\"id\", \"app_label\", \"model\") VALUES ({row_id}, {quote(app_label)}, {quote(model)});\n")
        for row_id, name, content_type_id, codename in permissions:
            handle.write(f"INSERT INTO \"auth_permission\" (\"id\", \"name\", \"content_type_id\", \"codename\") VALUES ({row_id}, {quote(name)}, {content_type_id}, {quote(codename)});\n")
        # The sequences have to follow the rows, or the next insert collides with one of them.
        handle.write("SELECT setval(pg_get_serial_sequence('django_content_type', 'id'), (SELECT MAX(id) FROM django_content_type));\n")
        handle.write("SELECT setval(pg_get_serial_sequence('auth_permission', 'id'), (SELECT MAX(id) FROM auth_permission));\n")


def quote(value):
    """A Postgres string literal, doubling any quote inside it."""
    escaped = value.replace("'", "''")
    return f"'{escaped}'"


def main():
    if len(sys.argv) != 2:
        raise SystemExit(f"usage: {sys.argv[0]} <output directory>")
    root = os.path.abspath(sys.argv[1])
    loader = MigrationLoader(connection, ignore_no_migrations=True)
    if loader.applied_migrations:
        raise SystemExit("this has to run against an empty database: it records what each migration does to the state the one before it left behind, and applies them as it goes")

    rows = []
    for app, name in plan(loader):
        migration = loader.disk_migrations[app, name]
        steps, ledger_ddl = record(app, name, migration)
        if ledger_ddl:
            with open(os.path.join(root, "ledger.sql"), "w") as handle:
                handle.write("-- The ledger table, which Django's MigrationRecorder creates before the first migration runs. Recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.\n")
                handle.write(ledger_ddl.strip().rstrip(";") + ";\n")

        directory = os.path.join(root, "sql", app)
        os.makedirs(directory, exist_ok=True)
        with open(os.path.join(directory, name + ".sql"), "w") as handle:
            handle.write(f"-- {app}.{name}, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.\n")
            for kind, value in steps:
                if kind == "run":
                    # Where the ported Go operation goes, in the position Django ran the Python one.
                    handle.write(f"-- RUN {app}.{name}.{value}\n")
                    continue
                one_line = value.strip().rstrip(";")
                if "\n" in one_line:
                    # The Go loader reads one statement per line, so a statement that wrapped would be split in the middle and both halves would fail. Django's schema editor builds these on one line; if that ever stops being true this has to be given a real separator rather than quietly producing something that cannot be parsed.
                    raise SystemExit(f"{app}.{name} produced a statement spanning lines, which the one-per-line format cannot carry:\n{one_line}")
                handle.write(one_line + ";\n")

        rows.append((app, name, ";".join(python_operations(migration)), "atomic" if migration.atomic else "non-atomic", len(steps)))

    with open(os.path.join(root, "plan.tsv"), "w") as handle:
        handle.write(
            "# Every migration apps/api applies, in the order Django applies it, generated by apps/api-go/tools/generate_migration_sql.py. "
            "Columns: app\tname\toperations that carry code rather than schema changes (semicolon separated)\twhether Django runs it in a transaction. Do not edit by hand.\n"
        )
        for app, name, operations, atomic, _ in rows:
            handle.write(f"{app}\t{name}\t{operations}\t{atomic}\n")

    write_signal_rows(root)

    coded = sum(1 for _, _, operations, _, _ in rows if operations)
    steps = sum(count for _, _, _, _, count in rows)
    print(f"{len(rows)} migrations, {steps} steps, {coded} carrying code to port", file=sys.stderr)


if __name__ == "__main__":
    main()
