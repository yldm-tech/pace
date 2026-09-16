package migrate

import (
	"context"
	"database/sql"
)

// The RunPython operations that come from Django and django-celery-beat rather than from Plane.
//
// There are two, and neither is ported in the ordinary sense. They are registered rather than left unported so that the refusal a real migration earns is not buried under two that were never going to need code.
func init() {
	// The forwards half of this one is migrations.RunPython.noop — Django's own do-nothing callable, there so the migration can be reversed. The plan calls it noop because that is the function's name.
	register("contenttypes", "0002_remove_content_type_name", "noop", nothingToDo)

	// update_proxy_model_permissions repoints the permissions of proxy models at the right content type. Everything it touches is in auth_permission and django_content_type, and the contents of both are not built up migration by migration here: they are written out at the end of a full run from signal_rows.sql, recorded from a database Django had finished migrating. Whatever this would have changed is already in that recording.
	register("auth", "0011_update_proxy_permissions", "update_proxy_model_permissions", nothingToDo)
}

// nothingToDo is for an operation whose work is either literally nothing or already accounted for elsewhere.
func nothingToDo(context.Context, *sql.Tx) error { return nil }
