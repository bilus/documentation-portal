package portal

import "testing"

func TestPageText(t *testing.T) {
	h := "<h1 id=\"pets\">Pets</h1>\n<p>Call <code>GET /pets</code>, see <a href=\"/docs/b.md\" rel=\"nofollow\">the guide</a>.</p>\n" +
		"<ol>\n<li>Sign in.</li>\n<li>List them:\n<ul>\n<li>all</li>\n</ul>\n</li>\n</ol>\n" +
		"<pre><code class=\"language-sh\">curl /pets\n# all of them\n</code></pre>\n" +
		"<table>\n<thead>\n<tr>\n<th>Field</th>\n<th>Type</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td>name</td>\n<td>string</td>\n</tr>\n</tbody>\n</table>\n" +
		"<h2 id=\"more\">More</h2>\n<p><img src=\"/raw/a.png\" alt=\"a pet\"><br>\nEnd.</p>\n<hr>\n<h1>Second</h1>\n"
	want := "# Pets\n\nCall `GET /pets`, see [the guide](/docs/b.md).\n\n1. Sign in.\n2. List them:\n  - all\n\n" +
		"```\ncurl /pets\n# all of them\n```\n\n| Field | Type |\n| name | string |\n\n" +
		"## More\n\n![a pet](/raw/a.png)\nEnd.\n\n---\n\n# Second"
	text, title := pageText(h)
	if text != want {
		t.Errorf("text:\n%s\nwant:\n%s", text, want)
	}
	if title != "Pets" {
		t.Errorf("title %q, want Pets", title)
	}
}
