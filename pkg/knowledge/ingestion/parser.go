package ingestion

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/ledongthuc/pdf"
	"golang.org/x/net/html"
)

// Parser extracts plain text from a raw document byte slice (FR-8).
type Parser interface {
	Parse(data []byte) (string, error)
}

// ParserFor returns the appropriate Parser for a given MIME/extension type.
func ParserFor(docType string) (Parser, error) {
	switch strings.ToLower(docType) {
	case "txt", "text/plain":
		return &txtParser{}, nil
	case "html", "text/html":
		return &htmlParser{}, nil
	case "pdf", "application/pdf":
		return &pdfParser{}, nil
	case "docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return &docxParser{}, nil
	default:
		return nil, fmt.Errorf("unsupported document type: %q", docType)
	}
}

// txtParser returns the raw bytes as UTF-8 text.
type txtParser struct{}

func (p *txtParser) Parse(data []byte) (string, error) { return string(data), nil }

// htmlParser strips HTML tags and returns visible text content.
type htmlParser struct{}

func (p *htmlParser) Parse(data []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("html parse: %w", err)
	}
	var sb strings.Builder
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.TextNode {
			s := strings.TrimSpace(n.Data)
			if s != "" {
				sb.WriteString(s)
				sb.WriteByte('\n')
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(doc)
	return sb.String(), nil
}

// pdfParser extracts text from PDF bytes using ledongthuc/pdf.
// Best-effort: not all PDF encodings are supported.
type pdfParser struct{}

func (p *pdfParser) Parse(data []byte) (string, error) {
	r := bytes.NewReader(data)
	pdfReader, err := pdf.NewReader(r, int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("pdf open: %w", err)
	}
	var sb strings.Builder
	for i := 1; i <= pdfReader.NumPage(); i++ {
		page := pdfReader.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			// skip unreadable pages rather than aborting
			continue
		}
		sb.WriteString(text)
		sb.WriteByte('\n')
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("pdf: no extractable text found (may be image-based)")
	}
	return sb.String(), nil
}

// docxParser extracts text from DOCX (Office Open XML) bytes.
// Reads word/document.xml and collects all <w:t> elements.
type docxParser struct{}

func (p *docxParser) Parse(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("docx: not a valid ZIP: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("docx: open document.xml: %w", err)
		}
		defer func() { _ = rc.Close() }()
		return extractDocxText(rc)
	}
	return "", fmt.Errorf("docx: word/document.xml not found in archive")
}

func extractDocxText(r io.Reader) (string, error) {
	var sb strings.Builder
	dec := xml.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("docx xml decode: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "t" {
			var text string
			if err := dec.DecodeElement(&text, &se); err == nil {
				sb.WriteString(text)
			}
		}
	}
	return sb.String(), nil
}
