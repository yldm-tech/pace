package htmlsanitizer

import "testing"

// The expectations below are the output of nh3 0.2.18 (the version pinned in
// apps/api/requirements/base.txt) running the policy from
// plane/utils/content_validator.py over the same input.
func TestCleanMatchesNh3(t *testing.T) {
	for _, test := range []struct {
		in   string
		want string
	}{
		{in: "<script>body</script>", want: ""},
		{in: "<style>body</style>", want: ""},
		{in: "<template>body</template>", want: ""},
		{in: "<p href=\"javascript:alert(1)\">body</p>", want: "<p>body</p>"},
		{in: "<p src=\"data:text/html,x\">body</p>", want: "<p>body</p>"},
		{in: "<p href=\"ftp://x\">body</p>", want: "<p>body</p>"},
		{in: "<p href=\"mailto:a@b.com\">body</p>", want: "<p>body</p>"},
		{in: "<p href=\"tel:+1\">body</p>", want: "<p>body</p>"},
		{in: "<p onclick=\"x()\">body</p>", want: "<p>body</p>"},
		{in: "<p data-unknown=\"1\">body</p>", want: "<p>body</p>"},
		{in: "<h7>body</h7>", want: "body"},
		{in: "<p>unclosed", want: "<p>unclosed</p>"},
		{in: "<table>body</table>", want: "body<table></table>"},
		{in: "<td>orphan</td>", want: "orphan"},
		{in: "<b><i>x</b></i>", want: "<b><i>x</i></b>"},
		{in: "<p>5 < 6</p>", want: "<p>5 &lt; 6</p>"},
		{in: "<p>a<br/>b</p>", want: "<p>a<br>b</p>"},
		{in: "<mention-component>body</mention-component>", want: "<mention-component>body</mention-component>"},
		{in: "<image-component>body</image-component>", want: "<image-component>body</image-component>"},
		{in: "<p checked>body</p>", want: "<p>body</p>"},
		{in: "<p TITLE=\"upper\">body</p>", want: "<p title=\"upper\">body</p>"},
		{in: "<p title='single'>body</p>", want: "<p title=\"single\">body</p>"},
		{in: "<div data-unknown=\"1\">body</div>", want: "<div>body</div>"},
		{in: "<p rel=\"nofollow\">body</p>", want: "<p>body</p>"},
		{in: "<p target=\"_blank\">body</p>", want: "<p>body</p>"},
		{in: "<li>orphan</li>", want: "<li>orphan</li>"},
		{in: "<p>&nbsp;</p>", want: "<p>&nbsp;</p>"},
		{in: "<span>&#x41;&#66;</span>", want: "<span>AB</span>"},
		{in: "<p>&unknownentity;</p>", want: "<p>&amp;unknownentity;</p>"},
		{in: "<pre>body</pre>", want: "<pre>body</pre>"},
		{in: "<p aspectRatio=\"1.5\">body</p>", want: "<p>body</p>"},
		{in: "<p colspan=\"2\">body</p>", want: "<p>body</p>"},
		{in: "<table><thead><tr><th colspan='2' background='#fff'>h</th></tr></thead><tbody><tr><td textColor='red'>c</td></tr></tbody></table>", want: "<table><thead><tr><th colspan=\"2\" background=\"#fff\">h</th></tr></thead><tbody><tr><td textcolor=\"red\">c</td></tr></tbody></table>"},
		{in: "<P CLASS='X'>upper tag</P>", want: "<p class=\"X\">upper tag</p>"},
		{in: "<p><!-- c --></p>", want: "<p></p>"},
		{in: "<p>中文 😀</p>", want: "<p>中文 😀</p>"},
		{in: "<img>body</img>", want: "<img>body"},
		{in: "<input>body</input>", want: "<input>body"},
		{in: "<label>body</label>", want: "<label>body</label>"},
		{in: "<center>body</center>", want: "<center>body</center>"},
		{in: "<strike>body</strike>", want: "<strike>body</strike>"},
		{in: "<wbr>body</wbr>", want: "<wbr>body"},
		{in: "<col>body</col>", want: "body"},
		{in: "<p href=\"/rel\">body</p>", want: "<p>body</p>"},
		{in: "<iframe>body</iframe>", want: "body"},
		{in: "<noscript>body</noscript>", want: "body"},
		{in: "<textarea>body</textarea>", want: "body"},
		{in: "<title>body</title>", want: "body"},
		{in: "<p language=\"go\">body</p>", want: "<p>body</p>"},
		{in: "<p>body</p>", want: "<p>body</p>"},
		{in: "<p class=\"c\">body</p>", want: "<p class=\"c\">body</p>"},
		{in: "<p style=\"color:red\">body</p>", want: "<p style=\"color:red\">body</p>"},
		{in: "<p href=\"http://a.com\">body</p>", want: "<p>body</p>"},
	} {
		if got := Clean(test.in); got != test.want {
			t.Errorf("Clean(%q)\n got  %q\n want %q", test.in, got, test.want)
		}
	}
}
