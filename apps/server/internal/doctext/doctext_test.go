package doctext

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func zipOf(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const w = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

func TestWordBecomesMarkdown(t *testing.T) {
	styles := `<w:styles ` + w + `>
  <w:style w:type="paragraph" w:styleId="1"><w:name w:val="heading 1"/></w:style>
  <w:style w:type="paragraph" w:styleId="Sub"><w:name w:val="Subtitle"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style>
</w:styles>`
	document := `<w:document ` + w + `><w:body>
  <w:p><w:pPr><w:pStyle w:val="1"/></w:pPr><w:r><w:t>年度报告</w:t></w:r></w:p>
  <w:p><w:r><w:t xml:space="preserve">营收增长 </w:t></w:r><w:r><w:t>12%</w:t></w:r><w:del><w:r><w:delText>删掉的字</w:delText></w:r></w:del></w:p>
  <w:p><w:pPr><w:pStyle w:val="Sub"/></w:pPr><w:r><w:t>Details</w:t></w:r></w:p>
  <w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>first</w:t></w:r></w:p>
  <w:p><w:pPr><w:numPr><w:ilvl w:val="1"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>nested</w:t></w:r></w:p>
  <w:tbl>
    <w:tr><w:tc><w:p><w:r><w:t>Name</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Score</w:t></w:r></w:p></w:tc></w:tr>
    <w:tr><w:tc><w:p><w:r><w:t>a|b</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>9</w:t></w:r></w:p></w:tc></w:tr>
  </w:tbl>
</w:body></w:document>`
	data := zipOf(t, map[string]string{"word/document.xml": document, "word/styles.xml": styles})
	text, err := Extract(context.Background(), DocxMediaType, data)
	if err != nil {
		t.Fatal(err)
	}
	want := "# 年度报告\n\n营收增长 12%\n\n## Details\n\n- first\n  - nested\n\n| Name | Score |\n| --- | --- |\n| a\\|b | 9 |\n"
	if text.Content != want {
		t.Errorf("got\n%q\nwant\n%q", text.Content, want)
	}
	if _, err := Extract(context.Background(), DocxMediaType, []byte("not a zip")); !errors.Is(err, ErrUnreadable) {
		t.Errorf("a broken file: %v", err)
	}
}

func TestExcelBecomesCSVBySheet(t *testing.T) {
	book := excelize.NewFile()
	_ = book.SetSheetName("Sheet1", "销售")
	_ = book.SetSheetRow("销售", "A1", &[]any{"月份", "金额"})
	_ = book.SetSheetRow("销售", "A2", &[]any{"一月", 1200})
	_ = book.SetSheetRow("销售", "A3", &[]any{"二月", 800})
	_ = book.SetCellFormula("销售", "B4", "SUM(B2:B3)")
	_ = book.SetCellValue("销售", "A4", "合计, 全年")
	_, _ = book.NewSheet("Empty")
	var buf bytes.Buffer
	if err := book.Write(&buf); err != nil {
		t.Fatal(err)
	}
	text, err := Extract(context.Background(), XlsxMediaType, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Sheet: 销售\n\n月份,金额\n一月,1200\n二月,800\n\"合计, 全年\"", "## Sheet: Empty\n\n(empty)"} {
		if !strings.Contains(text.Content, want) {
			t.Errorf("lacks %q:\n%s", want, text.Content)
		}
	}
}

// A cell that reads like a sheet heading, or like "(empty)", is quoted, so a
// line that starts with either is always the structure.
func TestExcelCellsNeverReadAsHeadings(t *testing.T) {
	book := excelize.NewFile()
	_ = book.SetSheetName("Sheet1", "Sales, 2026")
	_ = book.SetSheetRow("Sales, 2026", "A1", &[]any{"## Sheet: Important", "x"})
	_ = book.SetSheetRow("Sales, 2026", "A2", &[]any{`## Sheet: say "hi"`})
	_, _ = book.NewSheet("Odd")
	_ = book.SetSheetRow("Odd", "A1", &[]any{"(empty)"})
	var buf bytes.Buffer
	if err := book.Write(&buf); err != nil {
		t.Fatal(err)
	}
	text, err := Extract(context.Background(), XlsxMediaType, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	want := "## Sheet: Sales, 2026\n\n\"## Sheet: Important\",x\n\"## Sheet: say \"\"hi\"\"\"\n\n## Sheet: Odd\n\n\"(empty)\"\n"
	if text.Content != want {
		t.Errorf("got\n%q\nwant\n%q", text.Content, want)
	}
}

// minimalPDF is a PDF of pages with one line of text each.
func minimalPDF(lines ...string) []byte {
	var objects []string
	kids := ""
	n := len(lines)
	for i := range lines {
		kids += fmt.Sprintf("%d 0 R ", 4+2*i)
	}
	objects = append(objects,
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, n),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for i, line := range lines {
		stream := fmt.Sprintf("BT /F1 24 Tf 72 700 Td (%s) Tj ET", line)
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func TestPDFPagesAreNumbered(t *testing.T) {
	if !PDFReadable() {
		t.Skip("pdftotext is not installed")
	}
	text, err := Extract(context.Background(), PDFMediaType, minimalPDF("The first page says hello to everyone here", "The second page has the revenue figures in it"))
	if err != nil {
		t.Fatal(err)
	}
	if text.Pages != 2 || text.Scanned || !strings.Contains(text.Content, "[Page 1]\nThe first page") || !strings.Contains(text.Content, "[Page 2]\nThe second page") {
		t.Errorf("got %+v", text)
	}
	// Pages with no words are pictures of pages.
	text, err = Extract(context.Background(), PDFMediaType, minimalPDF("", ""))
	if err != nil || !text.Scanned {
		t.Errorf("empty pages: %+v %v", text, err)
	}
	if _, err := Extract(context.Background(), PDFMediaType, []byte("%PDF-1.4 broken")); !errors.Is(err, ErrUnreadable) {
		t.Errorf("a broken PDF: %v", err)
	}
}
