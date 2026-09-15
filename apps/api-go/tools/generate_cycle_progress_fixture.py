"""Dumps how the cycle progress body renders each aggregate result.

Five of the six point sums go through Python's `or 0`, and the sixth carries a Django default instead. The two spell zero differently in the body, which is the whole reason for the fixture.

Run from apps/api: python ../api-go/tools/generate_cycle_progress_fixture.py > ../api-go/internal/project/testdata/cycle_progress.tsv
"""

import json

# What the aggregate can hand back: null when the cycle holds no estimated issue at all, and a float otherwise — including a float that happens to be zero, which is what a cycle full of unestimated-but-pointed issues in other states produces.
CASES = [None, 0.0, -0.0, 1.0, 0.5, 2.0, 13.0, 0.1, 100.0, 1e16, 1e-5, -3.5, 65535.0]

print("aggregate\tor_zero\tcoalesced")
for value in CASES:
    # The five grouped sums.
    or_zero = value or 0
    # The total, which Django wraps in Coalesce(..., 0) with a FloatField output.
    coalesced = 0.0 if value is None else value
    print(
        "%s\t%s\t%s"
        % (
            json.dumps(value),
            json.dumps(or_zero),
            json.dumps(coalesced),
        )
    )
