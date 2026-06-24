package pdfextract

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeExtractorReadsPositionedText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.pdf")
	if err := os.WriteFile(path, minimalPDF("Hello scientific paper"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := New().Extract(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.PageCount != 1 {
		t.Fatalf("page count=%d", result.PageCount)
	}
	if size := result.PageSizes[1]; size.Width != 612 || size.Height != 792 {
		t.Fatalf("page size=%#v", size)
	}
	var combined strings.Builder
	for _, block := range result.Blocks {
		combined.WriteString(block.Text)
		combined.WriteByte(' ')
		if block.BBox == nil || block.PageNo != 1 {
			t.Fatalf("missing geometry: %#v", block)
		}
	}
	if !strings.Contains(combined.String(), "Hello scientific paper") {
		t.Fatalf("text not extracted: %q (%#v)", combined.String(), result.Blocks)
	}
}

func minimalPDF(text string) []byte {
	content := fmt.Sprintf("BT\n/F1 12 Tf\n72 720 Td\n(%s) Tj\nET\n", strings.ReplaceAll(text, ")", `\)`))
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
	}
	var buffer bytes.Buffer
	buffer.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = buffer.Len()
		fmt.Fprintf(&buffer, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := buffer.Len()
	fmt.Fprintf(&buffer, "xref\n0 %d\n", len(objects)+1)
	buffer.WriteString("0000000000 65535 f \n")
	for index := 1; index <= len(objects); index++ {
		fmt.Fprintf(&buffer, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&buffer, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buffer.Bytes()
}
