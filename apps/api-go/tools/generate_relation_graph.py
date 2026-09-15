"""Dump the Django model relation graph the Go worker needs.

plane.bgtasks.deletion_task.soft_delete_related_objects walks Django's model
metadata at runtime to find reverse relations and their on_delete behavior.
PostgreSQL cannot supply that: Django creates its foreign keys without ON DELETE
actions, so the catalog knows the graph but not whether a relation cascades or
nulls. This script exports the same metadata so the Go worker can follow it, and
CI re-runs it to fail when the models drift away from the committed copy.

Usage, from apps/api with the Django environment configured:

    python ../api-go/tools/generate_relation_graph.py > ../api-go/internal/worker/relation_graph.json
"""

import json
import os
import sys

# The script lives outside the Django project, so make the working directory
# importable; it is expected to be apps/api.
sys.path.insert(0, os.getcwd())

import django
from django.apps import apps
from django.db.models.fields.related import OneToOneRel


def audit_columns(meta):
    """Return the created_by / updated_by column names, when the model has them."""
    columns = {}
    for name in ("created_by", "updated_by"):
        try:
            columns[name] = meta.get_field(name).column
        except Exception:
            columns[name] = None
    return columns


def build_graph() -> dict:
    # BaseModel.save is the one that blanks created_by and updated_by when there
    # is no current user, which is always the case inside a Celery worker. Models
    # that stop at AuditModel, such as ProjectIdentifier, keep their audit
    # columns, so the two cases have to be told apart.
    from plane.db.models.base import BaseModel

    models = {}
    for model in apps.get_models():
        meta = model._meta
        key = f"{meta.app_label}.{meta.model_name}"

        relations = []
        # The same filter soft_delete_related_objects applies.
        for relation in meta.get_fields():
            if not ((relation.one_to_many or relation.one_to_one) and relation.auto_created and not relation.concrete):
                continue
            on_delete = getattr(relation.on_delete, "__name__", "")
            related_meta = relation.related_model._meta
            # remote_field.name is the field on the related model pointing back
            # here; its column is what the Go worker filters and nulls on.
            field = related_meta.get_field(relation.remote_field.name)
            relations.append(
                {
                    "accessor": relation.get_accessor_name(),
                    "on_delete": on_delete,
                    "one_to_one": isinstance(relation, OneToOneRel),
                    "related_model": f"{related_meta.app_label}.{related_meta.model_name}",
                    "related_table": related_meta.db_table,
                    "related_column": field.column,
                    "related_soft_deletes": any(f.name == "deleted_at" for f in related_meta.get_fields()),
                }
            )

        relations.sort(key=lambda item: (item["accessor"], item["related_model"]))
        columns = audit_columns(meta)
        models[key] = {
            "table": meta.db_table,
            "primary_key": meta.pk.column,
            "soft_deletes": any(f.name == "deleted_at" for f in meta.get_fields()),
            "clears_audit_user": issubclass(model, BaseModel),
            "created_by_column": columns["created_by"],
            "updated_by_column": columns["updated_by"],
            "relations": relations,
        }
    return {"django_version": django.get_version(), "models": dict(sorted(models.items()))}


def main() -> int:
    django.setup()
    json.dump(build_graph(), sys.stdout, indent=2, sort_keys=False)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
