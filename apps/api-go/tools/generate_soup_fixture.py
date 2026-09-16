"""Round-trip a set of descriptions through BeautifulSoup and print what it produces.

The Go side round-trips the same descriptions and diffs the two, which is what keeps the asset
copier's rewrite of description_html a port rather than a guess: whatever the copier stores is
bs4's reading of the markup, not the markup the editor wrote.

Run from apps/api with the project's settings on the path:

    DJANGO_SETTINGS_MODULE=plane.settings.production SECRET_KEY=x \
    REDIS_URL=redis://localhost:6379/ DATABASE_URL=postgresql://u:p@localhost:5432/plane \
    python ../api-go/tools/generate_soup_fixture.py > \
    ../api-go/internal/soup/testdata/round_trip.json
"""

import json
import os
import sys

sys.path.insert(0, os.getcwd())

from bs4 import BeautifulSoup  # noqa: E402

# What the editor really writes, followed by the things that break a serialiser.
CASES = [
    "",
    "<p>hello</p>",
    "<p>hello <strong>there</strong> and <em>again</em></p>",
    "<h1>One</h1><h2>Two</h2><h3>Three</h3>",
    "<ul><li>one</li><li>two</li></ul>",
    "<ol><li>one</li><li><p>nested</p></li></ol>",
    "<blockquote><p>quoted</p></blockquote>",
    "<pre><code>a &lt; b &amp;&amp; c > d</code></pre>",
    "<p><a href=\"https://example.test/?a=1&amp;b=2\">link</a></p>",
    "<hr><p>after</p>",
    "<p>line<br>break</p>",
    '<image-component id="11111111-1111-1111-1111-111111111111" src="22222222-2222-2222-2222-222222222222" width="35%"></image-component>',
    '<p>before</p><image-component src="asset-one"></image-component><p>after</p>',
    '<image-component src="one"/>',
    '<img src="https://example.test/a.png" alt="a">',
    '<mention-component id="m1" entity_identifier="u1" entity_name="user_mention"></mention-component>',
    "<table><tr><th>h</th></tr><tr><td>c</td></tr></table>",
    "<table><tbody><tr><td>c</td></tr></tbody></table>",
    "<p>unclosed",
    "<p>a<div>b</div></p>",
    "<div><p>a</p><p>b</p></div>",
    "<p>&amp; &lt; &gt; &quot; &#39; &nbsp;</p>",
    '<p title="a &quot;quoted&quot; word">t</p>',
    "<p title=\"it's here\">t</p>",
    '<div class="a" data-x=1 data-y>t</div>',
    "<p>Ünicode ☃ and an emoji 🎉</p>",
    "<!-- a comment --><p>after</p>",
    "<script>if (a < b && c) { }</script>",
    "<style>.a > .b { color: red }</style>",
    "<p>  leading and trailing  </p>",
    "<P>UPPERCASE</P>",
    "<p>text</p>loose text<p>more</p>",
    "<checkbox-component checked></checkbox-component>",
    '<p data-empty="">t</p>',
    "<p>a<p>b</p>",
    "</p><p>a</p>",
    "<div><p>a</div>",
    "<ul><li>a<li>b</ul>",
    "<td>loose cell</td>",
    "<p>a</p></div>",
    "<image-component src=one></image-component>",
    '<image-component src="a&amp;b"></image-component>',
    "<p>text with <b>bold <i>and italic</b> crossed</i></p>",
    "<textarea>a < b</textarea>",
    "<p>&#x1F600; &#128512;</p>",
    "<a href='single'>q</a>",
    "<p></p>",
    "<p><image-component src=\"inner\"></image-component></p>",
    '<div class="a  b">x</div>',
    '<div class="">x</div>',
    "<div class>x</div>",
    '<a rel="a  b" href="x">q</a>',
    '<td headers="a  b">c</td>',
    "<!DOCTYPE html><p>a</p>",
]

REPLACEMENTS = [
    # (html, tag, [(old, new), ...]) — what replace_asset_ids does to each description.
    (
        '<p>a</p><image-component src="old-one"></image-component><image-component src="other"></image-component>',
        "image-component",
        [("old-one", "new-one")],
    ),
    (
        '<image-component src="old-one"></image-component><image-component src="old-two"></image-component>',
        "image-component",
        [("old-one", "new-one"), ("old-two", "new-two")],
    ),
    ('<image-component src="kept"></image-component>', "image-component", [("absent", "new")]),
    ('<img src="old-one">', "image-component", [("old-one", "new-one")]),
]


def extract(html, tag):
    soup = BeautifulSoup(html, "html.parser")
    return [found.get("src") for found in soup.find_all(tag) if found.get("src")]


def replace(html, tag, pairs):
    soup = BeautifulSoup(html, "html.parser")
    for found in soup.find_all(tag):
        for old, new in pairs:
            if found.get("src") == old:
                found["src"] = new
    return str(soup)


print(
    json.dumps(
        {
            "round_trip": [
                {"html": case, "output": str(BeautifulSoup(case, "html.parser")), "sources": extract(case, "image-component")}
                for case in CASES
            ],
            "replacements": [
                {
                    "html": html,
                    "tag": tag,
                    "pairs": [{"old": old, "new": new} for old, new in pairs],
                    "output": replace(html, tag, pairs),
                }
                for html, tag, pairs in REPLACEMENTS
            ],
        },
        indent=2,
        ensure_ascii=False,
    )
)
