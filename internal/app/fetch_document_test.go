package app

import "testing"

func TestCompleteFeedDocumentEncodingsAndComments(t *testing.T) {
	for _, body := range []string{
		"\xef\xbb\xbf<?xml version=\"1.0\"?><rss><channel/></rss>\n<!-- end -->",
		"<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><rss><channel><title>caf\xe9</title></channel></rss>",
		"<feed xmlns=\"http://www.w3.org/2005/Atom\"><title>A &amp; B</title></feed>\n",
	} {
		if err := completeFeedDocument([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
}
