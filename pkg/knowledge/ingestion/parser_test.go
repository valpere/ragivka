package ingestion_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/valpere/ragivka/pkg/knowledge/ingestion"
)

// ---------------------------------------------------------------------------
// ParserFor dispatch
// ---------------------------------------------------------------------------

func TestParserFor_knownTypes(t *testing.T) {
	known := []string{"txt", "text/plain", "html", "text/html", "pdf", "application/pdf",
		"docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"}
	for _, typ := range known {
		p, err := ingestion.ParserFor(typ)
		if err != nil {
			t.Errorf("ParserFor(%q): unexpected error: %v", typ, err)
		}
		if p == nil {
			t.Errorf("ParserFor(%q): returned nil parser", typ)
		}
	}
}

func TestParserFor_unsupportedType(t *testing.T) {
	_, err := ingestion.ParserFor("application/octet-stream")
	if err == nil {
		t.Error("expected error for unsupported type, got nil")
	}
}

// ---------------------------------------------------------------------------
// TXT parser
// ---------------------------------------------------------------------------

func TestTxtParser_returnsRawContent(t *testing.T) {
	p, _ := ingestion.ParserFor("txt")
	input := "Hello, world!\nLine 2."
	got, err := p.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestTxtParser_emptyInput(t *testing.T) {
	p, _ := ingestion.ParserFor("txt")
	got, err := p.Parse([]byte{})
	if err != nil {
		t.Fatalf("Parse empty: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestTxtParser_unicodeContent(t *testing.T) {
	p, _ := ingestion.ParserFor("txt")
	input := "Привіт світ! 🌍"
	got, err := p.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse unicode: %v", err)
	}
	if got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

// ---------------------------------------------------------------------------
// HTML parser
// ---------------------------------------------------------------------------

func TestHtmlParser_stripsTagsAndExtractsText(t *testing.T) {
	p, _ := ingestion.ParserFor("html")
	input := `<html><body><h1>Title</h1><p>Body <b>text</b>.</p></body></html>`
	got, err := p.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse HTML: %v", err)
	}
	if strings.Contains(got, "<h1>") || strings.Contains(got, "<b>") {
		t.Errorf("HTML tags not stripped: %q", got)
	}
	if !strings.Contains(got, "Title") || !strings.Contains(got, "text") {
		t.Errorf("expected visible text in output, got: %q", got)
	}
}

func TestHtmlParser_emptyBody(t *testing.T) {
	p, _ := ingestion.ParserFor("html")
	got, err := p.Parse([]byte("<html><body></body></html>"))
	if err != nil {
		t.Fatalf("Parse empty HTML: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("expected empty output for empty HTML, got: %q", got)
	}
}

func TestHtmlParser_preservesNestedText(t *testing.T) {
	p, _ := ingestion.ParserFor("html")
	input := `<div><p>First <span>nested</span> text.</p><p>Second.</p></div>`
	got, err := p.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse nested HTML: %v", err)
	}
	if !strings.Contains(got, "First") || !strings.Contains(got, "nested") || !strings.Contains(got, "Second") {
		t.Errorf("expected all text nodes in output, got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// DOCX parser
// ---------------------------------------------------------------------------

// makeDocxBytes creates a minimal valid DOCX ZIP containing word/document.xml
// with the given text in a <w:t> element.
func makeDocxBytes(text string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("word/document.xml")
	xml := fmt.Sprintf(
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`+
			`<w:body><w:p><w:r><w:t>%s</w:t></w:r></w:p></w:body></w:document>`,
		text,
	)
	_, _ = f.Write([]byte(xml))
	_ = zw.Close()
	return buf.Bytes()
}

func TestDocxParser_extractsText(t *testing.T) {
	p, _ := ingestion.ParserFor("docx")
	data := makeDocxBytes("Hello from DOCX")
	got, err := p.Parse(data)
	if err != nil {
		t.Fatalf("Parse DOCX: %v", err)
	}
	if !strings.Contains(got, "Hello from DOCX") {
		t.Errorf("expected extracted text, got: %q", got)
	}
}

func TestDocxParser_multipleWtElements(t *testing.T) {
	p, _ := ingestion.ParserFor("docx")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("word/document.xml")
	_, _ = f.Write([]byte(
		`<?xml version="1.0" encoding="UTF-8"?>` +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:body>` +
			`<w:p><w:r><w:t>First</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t> Second</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	))
	_ = zw.Close()
	got, err := p.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("Parse multi-paragraph DOCX: %v", err)
	}
	if !strings.Contains(got, "First") || !strings.Contains(got, "Second") {
		t.Errorf("expected both paragraphs in output, got: %q", got)
	}
}

func TestDocxParser_notAZip(t *testing.T) {
	p, _ := ingestion.ParserFor("docx")
	_, err := p.Parse([]byte("this is not a zip file"))
	if err == nil {
		t.Error("expected error for non-ZIP input, got nil")
	}
}

func TestDocxParser_missingDocumentXml(t *testing.T) {
	p, _ := ingestion.ParserFor("docx")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("word/other.xml")
	_, _ = f.Write([]byte("<root/>"))
	_ = zw.Close()
	_, err := p.Parse(buf.Bytes())
	if err == nil {
		t.Error("expected error when word/document.xml is absent, got nil")
	}
}
