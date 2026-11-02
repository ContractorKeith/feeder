package app

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/ContractorKeith/feeder/internal/storage"
)

type outline struct {
	Text     string    `xml:"text,attr,omitempty"`
	Title    string    `xml:"title,attr,omitempty"`
	Type     string    `xml:"type,attr,omitempty"`
	URL      string    `xml:"xmlUrl,attr,omitempty"`
	Category string    `xml:"category,attr,omitempty"`
	Children []outline `xml:"outline"`
}
type opml struct {
	XMLName  xml.Name  `xml:"opml"`
	Version  string    `xml:"version,attr"`
	Outlines []outline `xml:"body>outline"`
}

func ImportOPML(r io.Reader) ([]storage.Feed, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFeedBytes {
		return nil, Invalid(errors.New("OPML exceeds 5 MiB limit"))
	}
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	var doc opml
	if err = dec.Decode(&doc); err != nil {
		return nil, Invalid(errors.New("invalid OPML"))
	}
	for {
		tok, e := dec.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, Invalid(errors.New("invalid OPML trailing data"))
		}
		switch v := tok.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(v)) == "" {
				continue
			}
		case xml.Comment:
			continue
		}
		return nil, Invalid(errors.New("invalid OPML trailing data"))
	}
	feeds := []storage.Feed{}
	var walk func([]outline, []string, int) error
	walk = func(outlines []outline, inherited []string, depth int) error {
		if depth > 100 {
			return Invalid(errors.New("OPML nesting exceeds 100 levels"))
		}
		for _, o := range outlines {
			tags := append([]string{}, inherited...)
			if o.Category != "" {
				tags = append(tags, strings.Split(o.Category, ",")...)
			}
			tags, e := Tags(tags)
			if e != nil {
				return e
			}
			if o.URL != "" {
				u, e := FeedURL(o.URL)
				if e != nil {
					return e
				}
				title := o.Title
				if title == "" {
					title = o.Text
				}
				feeds = append(feeds, storage.Feed{URL: u, Title: PlainText(title), Tags: tags})
			} else if o.Type == "rss" || o.Type == "atom" {
				return Invalid(errors.New("feed outline is missing xmlUrl"))
			}
			if e := walk(o.Children, tags, depth+1); e != nil {
				return e
			}
		}
		return nil
	}
	if err = walk(doc.Outlines, nil, 0); err != nil {
		return nil, err
	}
	return feeds, nil
}
func ExportOPML(feeds []storage.Feed) ([]byte, error) {
	doc := opml{Version: "2.0", Outlines: []outline{}}
	for _, f := range feeds {
		doc.Outlines = append(doc.Outlines, outline{Text: f.Title, Title: f.Title, Type: "rss", URL: f.URL, Category: strings.Join(f.Tags, ",")})
	}
	b, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(b, '\n')...), nil
}
