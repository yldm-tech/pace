"""Generate the truth table for the complex filter backend.

Each line is a tab-separated pair: the filter JSON exactly as a client would send it, and
what Django makes of it — either a canonical rendering of the Q object the backend builds,
or the error code and message it refuses with.

Run from apps/api with the project's settings on the path:

    DJANGO_SETTINGS_MODULE=plane.settings.production SECRET_KEY=x \
    REDIS_URL=redis://localhost:6379/ DATABASE_URL=postgresql://u:p@localhost:5432/plane \
    python ../api-go/tools/generate_complex_filter_fixture.py > \
    ../api-go/internal/complexfilters/testdata/filters.tsv
"""

import datetime
import json
import os
import sys
import uuid

sys.path.insert(0, os.getcwd())

import django  # noqa: E402

django.setup()

from django.db.models import Q  # noqa: E402
from rest_framework.exceptions import ValidationError as DRFValidationError  # noqa: E402

from plane.db.models import Issue  # noqa: E402
from plane.utils.filters import ComplexFilterBackend, IssueFilterSet  # noqa: E402


class View:
    filterset_class = IssueFilterSet


def render_value(value):
    if hasattr(value, "query"):
        # A filter whose value cleaned to nothing returns the queryset unchanged, and
        # build_combined_q wraps that as a subquery over every row — which is no filter
        # at all, written the long way round.
        return "<every-row>"
    if isinstance(value, uuid.UUID):
        return json.dumps(str(value))
    if isinstance(value, datetime.date):
        return json.dumps(value.isoformat())
    if isinstance(value, (list, tuple)):
        return "[" + ",".join(render_value(item) for item in value) + "]"
    return json.dumps(value)


def render(node):
    """Canonical rendering of a Q tree, with the children of each connector sorted so
    the two implementations do not have to agree on the order Django happens to build
    them in."""
    if isinstance(node, Q):
        children = sorted(render(child) for child in node.children)
        body = node.connector + "(" + ",".join(children) + ")"
        return "NOT(" + body + ")" if node.negated else body
    lookup, value = node
    return lookup + "=" + render_value(value)


A = "11111111-1111-1111-1111-111111111111"
B = "22222222-2222-2222-2222-222222222222"

CASES = [
    # Every filter the set declares, one at a time.
    {"priority": "high"},
    {"priority__exact": "urgent"},
    {"priority__in": "high,urgent"},
    {"priority__in": ["high", "urgent"]},
    {"state_group": "started"},
    {"state_group__exact": "backlog"},
    {"state_group__in": "started,backlog"},
    {"state_id": A},
    {"state_id__exact": A},
    {"state_id__in": A + "," + B},
    {"state_id__in": [A, B]},
    {"project_id": A},
    {"project_id__in": A + "," + B},
    {"created_by_id": A},
    {"created_by_id__in": A + "," + B},
    {"assignee_id": A},
    {"assignee_id__in": A + "," + B},
    {"cycle_id": A},
    {"cycle_id__in": A + "," + B},
    {"module_id": A},
    {"module_id__in": A + "," + B},
    {"label_id": A},
    {"label_id__in": A + "," + B},
    {"mention_id": A},
    {"mention_id__in": A + "," + B},
    {"subscriber_id": A},
    {"subscriber_id__in": A + "," + B},
    {"is_draft": True},
    {"is_draft": False},
    {"is_draft": "maybe"},
    {"is_archived": True},
    {"is_archived": False},
    {"is_archived": "true"},
    {"is_archived": "false"},
    {"is_archived": 1},
    {"is_archived": 0},
    {"start_date": "2026-01-02"},
    {"start_date__range": "2026-01-02,2026-02-03"},
    {"target_date": "2026-01-02"},
    {"target_date__range": "2026-01-02,2026-02-03"},
    {"created_at": "2026-01-02"},
    {"created_at__exact": "2026-01-02"},
    {"created_at__range": "2026-01-02,2026-02-03"},
    {"updated_at": "2026-01-02"},
    {"updated_at__range": "2026-01-02,2026-02-03"},
    # Several fields in one leaf.
    {"priority": "high", "state_group": "started"},
    {"assignee_id": A, "priority": "urgent"},
    # The three connectors.
    {"or": [{"priority": "high"}, {"priority": "low"}]},
    {"and": [{"priority": "high"}, {"state_group": "started"}]},
    {"not": {"priority": "high"}},
    {"OR": [{"priority": "high"}, {"priority": "low"}]},
    {"AND": [{"priority": "high"}]},
    {"NOT": {"priority": "high"}},
    {"or": [{"and": [{"priority": "high"}, {"is_draft": False}]}, {"not": {"state_group": "backlog"}}]},
    {"and": [{"or": [{"priority": "high"}, {"priority": "urgent"}]}, {"assignee_id": A}]},
    {"not": {"not": {"priority": "high"}}},
    {"or": [{"priority": "high"}]},
    # Depth: five is the most it takes.
    {"or": [{"or": [{"or": [{"or": [{"priority": "high"}]}]}]}]},
    {"or": [{"or": [{"or": [{"or": [{"or": [{"priority": "high"}]}]}]}]}]},
    # Refusals.
    {},
    {"or": []},
    {"and": []},
    {"or": {"priority": "high"}},
    {"and": "x"},
    {"not": [{"priority": "high"}]},
    {"not": "x"},
    {"or": [{"priority": "high"}], "and": [{"priority": "low"}]},
    {"or": [{"priority": "high"}], "priority": "low"},
    {"or": ["x"]},
    {"or": [{}]},
    {"nonexistent": 1},
    {"priority__gte": "high"},
    {"priority": []},
    {"priority": {"a": 1}},
    {"or": [{"priority": {"a": 1}}]},
    {"priority": "nonsense"},
    {"state_id": "not-a-uuid"},
    {"created_at": "not-a-date"},
    {"start_date__range": "2026-01-02"},
    {"priority": None},
    {"state_id": None},
    # Values that clean to nothing. A method filter short-circuits to the queryset, which
    # is no filter at all; a plain field filter still writes its condition.
    {"assignee_id": None},
    {"assignee_id": ""},
    {"state_id": ""},
    {"state_id__in": ""},
    {"priority__in": ""},
    {"state_group__in": ""},
    {"start_date": ""},
    {"created_at": ""},
    {"is_draft": None},
    {"is_archived": None},
    {"is_draft": 2},
    {"is_draft": 3},
    {"is_draft": "2"},
    # The uuid cleaner takes an unhyphenated one and trims what surrounds it.
    {"state_id": "11111111111111111111111111111111"},
    {"state_id": " 11111111-1111-1111-1111-111111111111 "},
    {"state_id__in": "11111111-1111-1111-1111-111111111111,"},
    {"state_id__in": ","},
    # The two range filters are not the same filter: one insists on a pair, the other does not.
    {"created_at__range": "2026-01-02"},
    {"created_at__range": "2026-01-02,2026-02-03,2026-03-04"},
    {"created_at__range": ""},
    {"start_date__range": ""},
    {"start_date__range": "2026-01-02,2026-02-03,2026-03-04"},
    # The dates Django's date field accepts beyond the obvious one.
    {"start_date": "2026-1-2"},
    {"start_date": "01/02/2026"},
    {"start_date": "2026-01-02T00:00:00Z"},
    # A choice that is not one, and one whose case is wrong.
    {"priority": "HIGH"},
    {"priority__in": "high,nonsense"},
    # Numbers and booleans where a string is expected.
    {"priority": 1},
    {"state_group": 1},
]


def main():
    backend = ComplexFilterBackend()
    queryset = Issue.issue_objects.all()
    view = View()
    print("# The complex filter backend's truth table, generated by "
          "apps/api-go/tools/generate_complex_filter_fixture.py. Do not edit by hand.")
    for case in CASES:
        encoded = json.dumps(case, sort_keys=True, separators=(",", ":"))
        try:
            backend._validate_structure(case, max_depth=5, current_depth=1) if case else None
            if case:
                backend._validate_fields(case, view)
            node = backend._evaluate_node(case, view, queryset) if case else None
            outcome = "NONE" if node is None else render(node)
        except DRFValidationError as error:
            detail = error.detail
            outcome = "ERR\t" + str(detail["code"]) + "\t" + str(detail["message"])
        print(encoded + "\t" + outcome)


if __name__ == "__main__":
    main()
