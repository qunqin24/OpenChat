package doctext

import (
	"bytes"
	"encoding/csv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// A workbook is read sheet by sheet, each under a "## Sheet: name" heading, as
// CSV: compact, and a form every model reads. Cells are what Excel shows -
// formatted numbers and dates, and a formula's last computed value - and
// empty rows and columns at the ends of a sheet are left out; a sheet with no
// rows says "(empty)".
//
// The headings and "(empty)" are lines of their own, unquoted, which the file
// panel reads back (web/src/lib/workbook.ts). A row whose first cell would
// read as one of them has that cell quoted, so a line that starts with either
// is always the structure, never a cell.

// xlsxUnzipBytes bounds what a workbook may unpack to.
const xlsxUnzipBytes = 128 << 20

const (
	sheetHeading = "## Sheet: "
	emptySheet   = "(empty)"
)

func xlsx(data []byte) (Text, error) {
	book, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{
		UnzipSizeLimit:    xlsxUnzipBytes,
		UnzipXMLSizeLimit: xlsxUnzipBytes / 4,
	})
	if err != nil {
		return Text{}, ErrUnreadable
	}
	defer book.Close()
	var b strings.Builder
	for _, sheet := range book.GetSheetList() {
		rows, err := book.GetRows(sheet)
		if err != nil {
			return Text{}, ErrUnreadable
		}
		for len(rows) > 0 && blankRow(rows[len(rows)-1]) {
			rows = rows[:len(rows)-1]
		}
		b.WriteString(sheetHeading + sheet + "\n\n")
		if len(rows) == 0 {
			b.WriteString(emptySheet + "\n\n")
			continue
		}
		var sheetCSV bytes.Buffer
		writer := csv.NewWriter(&sheetCSV)
		for _, row := range rows {
			for len(row) > 0 && strings.TrimSpace(row[len(row)-1]) == "" {
				row = row[:len(row)-1]
			}
			if err := writeRow(writer, &sheetCSV, row); err != nil {
				return Text{}, ErrUnreadable
			}
			if sheetCSV.Len() > MaxTextBytes {
				return Text{}, ErrTooLong
			}
		}
		writer.Flush()
		b.Write(sheetCSV.Bytes())
		b.WriteString("\n")
		if b.Len() > MaxTextBytes {
			return Text{}, ErrTooLong
		}
	}
	return Text{Content: strings.TrimSpace(b.String()) + "\n"}, nil
}

func blankRow(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

// writeRow writes a row as CSV, quoting a first cell that would otherwise read
// as a sheet heading or as "(empty)". The CSV writer quotes only what needs it,
// and such a cell, unquoted, is written as it is: it is put back quoted.
func writeRow(writer *csv.Writer, out *bytes.Buffer, row []string) error {
	if len(row) == 0 || (!strings.HasPrefix(row[0], sheetHeading) && row[0] != emptySheet) {
		return writer.Write(row)
	}
	writer.Flush()
	start := out.Len()
	if err := writer.Write(row); err != nil {
		return err
	}
	writer.Flush()
	line := out.Bytes()[start:]
	if bytes.HasPrefix(line, []byte(row[0])) {
		rest := append([]byte{}, line[len(row[0]):]...)
		out.Truncate(start)
		out.WriteString(`"` + strings.ReplaceAll(row[0], `"`, `""`) + `"`)
		out.Write(rest)
	}
	return writer.Error()
}
