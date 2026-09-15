"""Dumps what a view's query column becomes for a given filters blob.

issue_filters is called with the decoded JSON body rather than a query string, so a value is usually a list and occasionally a string, and the POST branch stores it as it arrived. That is a different shape from the GET path the issue lists use, and it is covered separately here.

A relative date term resolves to a datetime.date, which the JSONField cannot hold, so the save raises. Those rows are recorded as RAISES rather than dropped.

The clock is frozen for the same reason the sibling generator freezes it: without that the fixture would go stale every morning.

Run from apps/api: python ../api-go/tools/generate_view_query_fixture.py > ../api-go/internal/project/testdata/view_query.tsv
"""

import datetime
import json
import os
import types

# The day the relative terms are measured from, matching the constant the Go test uses.
FROZEN_TODAY = datetime.date(2026, 6, 15)

SOURCE = os.path.join(os.getcwd(), "plane", "utils", "issue_filters.py")


def load_module():
    """Load issue_filters with its clock frozen and without standing Django up, the way the sibling generator does."""
    source = open(SOURCE).read()
    source = source.replace("from django.utils import timezone", "")
    source = source.replace("now = timezone.now().date()", "now = FROZEN_TODAY")
    module = types.ModuleType("issue_filters_frozen")
    module.FROZEN_TODAY = FROZEN_TODAY
    exec(compile(source, SOURCE, "exec"), module.__dict__)
    return module


issue_filters = load_module().issue_filters

UUID_ONE = "ae3b0f5e-6c9b-4b6e-8b8a-0f2a1c3d4e5f"
UUID_TWO = "b1c2d3e4-f5a6-4b7c-8d9e-0f1a2b3c4d5e"

CASES = [
    {},
    {"state": [UUID_ONE]},
    {"state": [UUID_ONE, UUID_TWO]},
    {"state": []},
    {"state": None},
    {"state": "null"},
    {"state": "not-a-uuid"},
    {"state_group": ["backlog", "started"]},
    {"estimate_point": ["1", "2"]},
    {"priority": ["urgent", "high", "none"]},
    {"parent": [UUID_ONE]},
    {"labels": [UUID_ONE, "None"]},
    {"labels": []},
    {"assignees": [UUID_ONE]},
    {"assignees": []},
    {"mentions": [UUID_ONE]},
    {"created_by": [UUID_ONE]},
    {"logged_by": [UUID_ONE]},
    {"project": [UUID_ONE]},
    {"cycle": [UUID_ONE]},
    {"cycle": []},
    {"module": [UUID_ONE]},
    {"module": []},
    {"subscriber": [UUID_ONE]},
    {"subscriber": []},
    {"inbox_status": ["1"]},
    {"intake_status": ["1"]},
    {"intake_status": ["1"], "inbox_status": ["2"]},
    {"name": "bug"},
    {"name": ""},
    {"created_at": ["2026-01-01;after"]},
    {"created_at": ["2026-01-01;before"]},
    {"created_at": ["2026-01-01;after", "2026-03-01;before"]},
    {"created_at": ["2026-01-01"]},
    {"created_at": []},
    {"created_at": "2026-01-01;after"},
    {"created_at": ["2_weeks;after;fromnow"]},
    {"updated_at": ["2026-02-02;before"]},
    {"completed_at": ["2026-02-02;after"]},
    {"start_date": ["2026-01-01;after"]},
    {"target_date": ["2026-01-01;before"]},
    {"start_date": []},
    {"type": "backlog"},
    {"type": "active"},
    {"type": "all"},
    {"type": "anything-else"},
    {"sub_issue": "true"},
    {"sub_issue": "false"},
    {"sub_issue": []},
    {"start_target_date": "true"},
    {"start_target_date": "false"},
    {"state": [UUID_ONE], "priority": ["urgent"], "labels": [UUID_TWO], "name": "crash"},
    {"assignees": [UUID_ONE], "module": [UUID_TWO], "cycle": [UUID_ONE], "subscriber": [UUID_TWO]},
]

print("filters\tquery")
for case in CASES:
    try:
        result = issue_filters(dict(case), "POST")
        rendered = json.dumps(result, sort_keys=True)
    except Exception:
        rendered = "RAISES"
    else:
        # The save is what refuses a value the column cannot hold, not the filter call.
        try:
            json.dumps(result)
        except TypeError:
            rendered = "RAISES"
    print("%s\t%s" % (json.dumps(case, sort_keys=True), rendered))
