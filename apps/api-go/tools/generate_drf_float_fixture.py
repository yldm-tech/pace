"""Dumps how Python renders a float in a JSON body, which is repr(), not Go's shortest-with-a-plain-point.

Run from apps/api-go: python3 tools/generate_drf_float_fixture.py > internal/drf/testdata/float_rendering.tsv
"""

import json
import random
import struct
import sys

CASES = [
    0.0,
    -0.0,
    1.0,
    -1.0,
    0.5,
    2.5,
    -2.5,
    65535.0,
    65536.0,
    1000.0,
    10000.0,
    100000.0,
    1000000.0,
    10000000.0,
    1e15,
    1e16,
    1e17,
    1.5e16,
    9007199254740992.0,
    1e21,
    1e22,
    1.2345678901234567e20,
    0.1,
    0.2,
    0.3,
    0.1 + 0.2,
    1.0 / 3.0,
    0.001,
    0.0001,
    1e-05,
    1e-06,
    1.5e-7,
    2.2250738585072014e-308,
    5e-324,
    1.7976931348623157e308,
    123.456,
    -123.456,
    3.14159265358979,
    1234567.891,
    99999999999999999.0,
    0.30000000000000004,
]

random.seed(20260916)
while len(CASES) < 400:
    # Uniform over the bit patterns, filtered to the finite values, which is the only way to reach the awkward ones.
    candidate = struct.unpack("<d", struct.pack("<Q", random.getrandbits(64)))[0]
    if candidate != candidate or candidate in (float("inf"), float("-inf")):
        continue
    CASES.append(candidate)

for _ in range(60):
    CASES.append(round(random.uniform(-1000, 1000), random.randint(0, 6)))
    CASES.append(float(random.randint(-(10**12), 10**12)))

print("bits\trendered")
seen = set()
for value in CASES:
    bits = struct.unpack("<Q", struct.pack("<d", value))[0]
    if bits in seen:
        continue
    seen.add(bits)
    # json.dumps uses float.__repr__ for a float, which is what a DRF body carries.
    rendered = json.dumps(value)
    assert rendered == repr(value), (rendered, repr(value))
    print("%d\t%s" % (bits, rendered))
