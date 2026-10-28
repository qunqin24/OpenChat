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
// empty rows and columns at the ends of a sheet are left out.

// xlsxUnzipBytes bounds what a workbook may unpack to.
const xlsxUnzipBytes = 128 << 20

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
		b.WriteString("## Sheet: " + sheet + "\n\n")
		if len(rows) == 0 {
			b.WriteString("(empty)\n\n")
			continue
		}
		var sheetCSV bytes.Buffer
		writer := csv.NewWriter(&sheetCSV)
		for _, row := range rows {
			for len(row) > 0 && strings.TrimSpace(row[len(row)-1]) == "" {
				row = row[:len(row)-1]
			}
			if err := writer.Write(row); err != nil {
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
