"""Dumps how analytics_plot.sort_data orders a chart's buckets.

A priority axis is reported in a fixed order and everything the order does not name is dropped; every other axis is sorted with the literal key "none" last. Neither is obvious from reading it, and the dropping in particular is easy to lose.

Run from apps/api: python ../api-go/tools/generate_sort_data_fixture.py > ../api-go/internal/project/testdata/sort_data.tsv
"""

import json
import os
import random
import types

SOURCE = os.path.join(os.getcwd(), "plane", "utils", "analytics_plot.py")


def load_sort_data():
    """Lift sort_data out of the module without standing Django up."""
    source = open(SOURCE).read()
    start = source.index("def sort_data(")
    end = source.index("def build_graph_plot(")
    module = types.ModuleType("sort_data_only")
    exec(compile(source[start:end], SOURCE, "exec"), module.__dict__)
    return module.sort_data


sort_data = load_sort_data()

KEY_POOLS = [
    ["low", "medium", "high", "urgent", "none"],
    ["urgent", "low"],
    ["none"],
    ["none", "backlog", "started"],
    ["2026-1", "2026-10", "2026-2", "2025-12"],
    ["b", "a", "none", "c"],
    ["B", "a", "C", "none"],
    ["", "a", "none"],
    ["nonexistent", "low"],
    ["Low", "low"],
    [],
    ["1", "10", "2"],
    ["none", "None"],
]

AXES = ["priority", "state_id", "created_at", "labels__id"]

random.seed(20260916)
CASES = []
for pool in KEY_POOLS:
    for axis in AXES:
        CASES.append((axis, pool))
for _ in range(40):
    size = random.randint(0, 6)
    pool = random.sample(["low", "medium", "high", "urgent", "none", "a", "b", "z", "2026-3", ""], size)
    CASES.append((random.choice(AXES), pool))

print("axis\tkeys\tordered")
for axis, keys in CASES:
    data = {key: [{"dimension": key}] for key in keys}
    ordered = list(sort_data(dict(data), axis).keys())
    print("%s\t%s\t%s" % (axis, json.dumps(keys), json.dumps(ordered)))
