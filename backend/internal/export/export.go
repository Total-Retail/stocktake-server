package export

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/totalretail/stocktake/internal/finalcount"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

// CountRow is a single item's final counted quantity with metadata.
type CountRow struct {
	ItemNo         string
	Description    string
	UoM            string
	CountedQty     float64
	TheoreticalQty float64
	Variance       float64
	UnitCost       float64
	VarianceCost   float64
}

// GenerateSessionExport builds an Excel workbook for the session's final count results,
// saves it to exportDir/session-{sessionID}.xlsx, and returns the file path.
// The format mirrors the LS worksheet layout expected by the finance team.
//
// NOTE: Run `go mod tidy` after adding github.com/xuri/excelize/v2 to go.mod.
func GenerateSessionExport(ctx context.Context, db *gorm.DB, sessionID, storeName, countDate, countType, exportDir string) (string, error) {
	// Ensure the directory exists
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return "", fmt.Errorf("create export dir: %w", err)
	}

	// Query final count quantities (latest round per item)
	type rawRow struct {
		ItemNo         string
		Description    string
		UoM            string
		CountedQty     float64
		TheoreticalQty float64
		UnitCost       float64
	}
	var rows []rawRow
	// Counted quantity: the same final figure that is posted and shown (internal/finalcount).
	if err := db.WithContext(ctx).Raw(finalcount.CTE+`
		SELECT
			si.item_no,
			si.description,
			si.uo_m,
			COALESCE(fc.total_qty, 0) AS counted_qty,
			COALESCE(ts.theoretical_qty, 0) AS theoretical_qty,
			si.unit_cost
		FROM session_items si
		LEFT JOIN theoretical_stocks ts ON ts.session_id = si.session_id AND ts.item_no = si.item_no
		LEFT JOIN final_counts fc ON fc.item_no = si.item_no
		WHERE si.session_id = ?
		ORDER BY si.item_no`, sessionID, sessionID).Scan(&rows).Error; err != nil {
		return "", fmt.Errorf("query count data: %w", err)
	}

	f := excelize.NewFile()
	defer f.Close()

	sheet := "Stock Count"
	f.SetSheetName("Sheet1", sheet)

	// ── Title block ────────────────────────────────────────────────────────────
	titleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 14},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1D9E75"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Border: []excelize.Border{
			{Type: "bottom", Color: "0F6E56", Style: 1},
		},
	})
	numStyle, _ := f.NewStyle(&excelize.Style{
		NumFmt: 4, // #,##0.00
	})
	negStyle, _ := f.NewStyle(&excelize.Style{
		NumFmt: 4,
		Font:   &excelize.Font{Color: "C00000"},
	})

	f.MergeCell(sheet, "A1", "H1")
	f.SetCellValue(sheet, "A1", "Stock Count Export — "+storeName)
	f.SetCellStyle(sheet, "A1", "H1", titleStyle)

	f.SetCellValue(sheet, "A2", "Date:")
	f.SetCellValue(sheet, "B2", countDate)
	f.SetCellValue(sheet, "C2", "Type:")
	f.SetCellValue(sheet, "D2", countType)
	f.SetCellValue(sheet, "E2", "Generated:")
	f.SetCellValue(sheet, "F2", time.Now().Format("2006-01-02 15:04"))

	// ── Column headers (row 4) ─────────────────────────────────────────────────
	headers := []string{"Item No.", "Description", "UoM", "Counted Qty", "Theoretical Qty", "Variance", "Unit Cost", "Variance Cost"}
	cols := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	widths := []float64{14, 40, 8, 14, 16, 12, 12, 14}
	for i, h := range headers {
		cell := cols[i] + "4"
		f.SetCellValue(sheet, cell, h)
		f.SetCellStyle(sheet, cell, cell, headerStyle)
		f.SetColWidth(sheet, cols[i], cols[i], widths[i])
	}

	// ── Data rows ──────────────────────────────────────────────────────────────
	for ri, r := range rows {
		row := ri + 5
		variance := r.CountedQty - r.TheoreticalQty
		varianceCost := variance * r.UnitCost
		rowStr := fmt.Sprintf("%d", row)

		f.SetCellValue(sheet, "A"+rowStr, r.ItemNo)
		f.SetCellValue(sheet, "B"+rowStr, r.Description)
		f.SetCellValue(sheet, "C"+rowStr, r.UoM)
		f.SetCellValue(sheet, "D"+rowStr, r.CountedQty)
		f.SetCellValue(sheet, "E"+rowStr, r.TheoreticalQty)
		f.SetCellValue(sheet, "F"+rowStr, variance)
		f.SetCellValue(sheet, "G"+rowStr, r.UnitCost)
		f.SetCellValue(sheet, "H"+rowStr, varianceCost)

		f.SetCellStyle(sheet, "D"+rowStr, "H"+rowStr, numStyle)
		if variance < 0 || varianceCost < 0 {
			f.SetCellStyle(sheet, "F"+rowStr, "F"+rowStr, negStyle)
			f.SetCellStyle(sheet, "H"+rowStr, "H"+rowStr, negStyle)
		}
	}

	// ── Totals row ─────────────────────────────────────────────────────────────
	if len(rows) > 0 {
		totRow := fmt.Sprintf("%d", len(rows)+5)
		totalStyle, _ := f.NewStyle(&excelize.Style{
			Font:   &excelize.Font{Bold: true},
			NumFmt: 4,
		})
		f.SetCellValue(sheet, "A"+totRow, "TOTAL")
		dataStart := 5
		dataEnd := len(rows) + 4
		for _, col := range []string{"D", "E", "F", "H"} {
			formula := fmt.Sprintf("SUM(%s%d:%s%d)", col, dataStart, col, dataEnd)
			f.SetCellFormula(sheet, col+totRow, formula)
			f.SetCellStyle(sheet, col+totRow, col+totRow, totalStyle)
		}
	}

	// ── Freeze header rows ────────────────────────────────────────────────────
	f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      4,
		TopLeftCell: "A5",
		ActivePane:  "bottomLeft",
	})

	// ── Save ───────────────────────────────────────────────────────────────────
	fileName := fmt.Sprintf("session-%s.xlsx", sessionID)
	filePath := filepath.Join(exportDir, fileName)
	if err := f.SaveAs(filePath); err != nil {
		return "", fmt.Errorf("save export file: %w", err)
	}
	return filePath, nil
}
