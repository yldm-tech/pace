"""Render the notification email template with Django and print what it produces.

The Go side renders the same file with the same contexts and diffs the two, which is what
keeps the template a copy rather than a translation.

Run from apps/api with the project's settings on the path:

    DJANGO_SETTINGS_MODULE=plane.settings.production SECRET_KEY=x \
    REDIS_URL=redis://localhost:6379/ DATABASE_URL=postgresql://u:p@localhost:5432/plane \
    python ../api-go/tools/generate_django_template_fixture.py > \
    ../api-go/internal/djangotemplate/testdata/issue_updates.txt
"""

import json
import os
import sys

sys.path.insert(0, os.getcwd())

import django  # noqa: E402

django.setup()

from django.template.loader import render_to_string  # noqa: E402

ACTOR = {
    "avatar_url": "https://example.test/a.png",
    "first_name": "Ada",
    "last_name": "Lovelace",
}

CASES = {
    "one actor, one change": {
        "data": [{
            "actor_detail": ACTOR,
            "changes": {"name": {"old_value": ["Before"], "new_value": ["After"]}},
            "issue_details": {"name": "Hello", "identifier": "ACME-7"},
            "activity_time": "10:30 AM",
        }],
        "summary": "Updates were made to the issue by",
        "actors_involved": 1,
        "issue": {"issue_identifier": "ACME-7", "name": "Hello", "issue_url": "https://example.test/i"},
        "receiver": {"email": "reader@example.test"},
        "issue_url": "https://example.test/i",
        "project_url": "https://example.test/p",
        "workspace": "acme",
        "project": "Website",
        "user_preference": "https://example.test/prefs",
        "comments": [],
        "entity_type": "issue",
    },
    "several actors, several fields": {
        "data": [{
            "actor_detail": {"avatar_url": "", "first_name": "Bob", "last_name": "Brown"},
            "changes": {
                "state": {"old_value": ["Backlog"], "new_value": ["In Progress"]},
                "assignees": {"old_value": ["Ada", "Bob", "Cat"], "new_value": ["Dan", "Eve"]},
                "labels": {"old_value": [], "new_value": ["bug", "urgent", "p1"]},
                "target_date": {"old_value": ["2026-01-02"], "new_value": ["2026-02-03"]},
                "priority": {"old_value": ["low"], "new_value": ["urgent"]},
                "duplicate": {"old_value": ["ACME-1", "ACME-2", "ACME-3"], "new_value": ["ACME-4"]},
            },
            "issue_details": {"name": "Something <script>", "identifier": "ACME-8"},
            "activity_time": "11:00 AM",
        }],
        "summary": "Updates were made to the issue by",
        "actors_involved": 3,
        "issue": {"issue_identifier": "ACME-8", "name": "Something <script> & more", "issue_url": "https://example.test/i"},
        "receiver": {"email": "reader@example.test"},
        "issue_url": "https://example.test/i",
        "project_url": "https://example.test/p",
        "workspace": "acme",
        "project": "Website & Co",
        "user_preference": "https://example.test/prefs",
        "comments": [{
            "actor_comments": {"old_value": ["was"], "new_value": ["<p>now</p>"]},
            "actor_detail": ACTOR,
        }],
        "entity_type": "issue",
    },
    "comments only": {
        "data": [],
        "summary": "Updates were made to the issue by",
        "actors_involved": 1,
        "issue": {"issue_identifier": "ACME-9", "name": "Quiet", "issue_url": "https://example.test/i"},
        "receiver": {"email": "reader@example.test"},
        "issue_url": "https://example.test/i",
        "project_url": "https://example.test/p",
        "workspace": "acme",
        "project": "Website",
        "user_preference": "https://example.test/prefs",
        "comments": [{"actor_comments": {"new_value": ["<p>hello</p>"]}, "actor_detail": ACTOR}],
        "entity_type": "issue",
    },
    "no actors at all": {
        "data": [],
        "summary": "Updates were made to the issue by",
        "actors_involved": 0,
        "issue": {"issue_identifier": "ACME-1", "name": "Empty", "issue_url": "https://example.test/i"},
        "receiver": {"email": "reader@example.test"},
        "issue_url": "https://example.test/i",
        "project_url": "https://example.test/p",
        "workspace": "acme",
        "project": "Website",
        "user_preference": "https://example.test/prefs",
        "comments": [],
        "entity_type": "issue",
    },
    "every state name the template knows": {
        "data": [
            {
                "actor_detail": ACTOR,
                "changes": {"state": {"old_value": [name], "new_value": [other]}},
                "issue_details": {"name": "S", "identifier": "ACME-2"},
                "activity_time": "09:00 AM",
            }
            for name, other in [
                ("Backlog", "In Progress"),
                ("In Progress", "Done"),
                ("Done", "Cancelled"),
                ("Cancelled", "Backlog"),
                ("Something Else", "Another"),
            ]
        ],
        "summary": "Updates were made to the issue by",
        "actors_involved": 5,
        "issue": {"issue_identifier": "ACME-2", "name": "States", "issue_url": "https://example.test/i"},
        "receiver": {"email": "reader@example.test"},
        "issue_url": "https://example.test/i",
        "project_url": "https://example.test/p",
        "workspace": "acme",
        "project": "Website",
        "user_preference": "https://example.test/prefs",
        "comments": [],
        "entity_type": "issue",
    },
    "long lists that the slice filter trims": {
        "data": [{
            "actor_detail": ACTOR,
            "changes": {
                "duplicate": {"old_value": ["a", "b", "c", "d", "e"], "new_value": ["f", "g", "h"]},
                "relates_to": {"old_value": ["i", "j", "k"], "new_value": ["l", "m", "n", "o"]},
                "blocking": {"old_value": ["p"], "new_value": ["q", "r"]},
                "blocked_by": {"old_value": [], "new_value": ["s", "t", "u"]},
                "modules": {"old_value": ["v", "w"], "new_value": ["x"]},
                "cycles": {"old_value": ["y"], "new_value": ["z"]},
                "estimate_point": {"old_value": ["1"], "new_value": ["5"]},
                "start_date": {"old_value": ["2026-01-01"], "new_value": ["2026-01-09"]},
            },
            "issue_details": {"name": "L", "identifier": "ACME-3"},
            "activity_time": "12:00 PM",
        }],
        "summary": "Updates were made to the issue by",
        "actors_involved": 1,
        "issue": {"issue_identifier": "ACME-3", "name": "Lists", "issue_url": "https://example.test/i"},
        "receiver": {"email": "reader@example.test"},
        "issue_url": "https://example.test/i",
        "project_url": "https://example.test/p",
        "workspace": "acme",
        "project": "Website",
        "user_preference": "https://example.test/prefs",
        "comments": [],
        "entity_type": "issue",
    },
    "values needing escaping": {
        "data": [{
            "actor_detail": {"avatar_url": "", "first_name": "A&B", "last_name": "<script>"},
            "changes": {"name": {"old_value": ["it's <b>bold</b>"], "new_value": ['say "hi" & bye']}},
            "issue_details": {"name": "<img>", "identifier": "ACME-4"},
            "activity_time": "01:00 PM",
        }],
        "summary": "Updates & things",
        "actors_involved": 1,
        "issue": {"issue_identifier": "ACME-4", "name": "it's <b>bold</b>", "issue_url": "https://example.test/i?a=1&b=2"},
        "receiver": {"email": "reader@example.test"},
        "issue_url": "https://example.test/i?a=1&b=2",
        "project_url": "https://example.test/p",
        "workspace": "acme&co",
        "project": "Web<site>",
        "user_preference": "https://example.test/prefs",
        "comments": [],
        "entity_type": "issue",
    },
}


def main():
    print("# The notification email template rendered by Django, generated by "
          "apps/api-go/tools/generate_django_template_fixture.py. Do not edit by hand.")
    for name, context in CASES.items():
        rendered = render_to_string("emails/notifications/issue-updates.html", context)
        print("=== CASE " + name)
        print("--- CONTEXT")
        print(json.dumps(context, sort_keys=True, separators=(",", ":")))
        print("--- OUTPUT")
        print(rendered)
        print("=== END")


if __name__ == "__main__":
    main()
