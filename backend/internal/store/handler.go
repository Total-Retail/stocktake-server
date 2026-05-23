package store

import (
	"encoding/csv"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Handler struct{ svc Service }

func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/stores", h.ListStores)
	rg.POST("/stores", h.CreateStore)
	rg.GET("/stores/:id", h.GetStore)
	rg.PUT("/stores/:id", h.UpdateStore)
	rg.GET("/stores/:id/layout", h.GetLayout)
	rg.POST("/stores/:id/areas", h.CreateArea)
	rg.POST("/stores/:id/aisles", h.CreateAisle)
	rg.POST("/stores/:id/bins", h.CreateBin)
	rg.POST("/stores/:id/layout/import", h.ImportLayout)
	rg.GET("/stores/:id/labels", h.GetAllLabels)
	rg.GET("/stores/:id/bins/:bin_id/label", h.GetBinLabel)
}

func (h *Handler) ListStores(c *gin.Context) {
	stores, err := h.svc.ListStores(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stores)
}

func (h *Handler) GetStore(c *gin.Context) {
	store, err := h.svc.GetStore(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "store not found"})
		return
	}
	c.JSON(http.StatusOK, store)
}

func (h *Handler) CreateStore(c *gin.Context) {
	var s Store
	if err := c.ShouldBindJSON(&s); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	created, err := h.svc.CreateStore(c.Request.Context(), s)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) UpdateStore(c *gin.Context) {
	var s Store
	if err := c.ShouldBindJSON(&s); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.ID = c.Param("id")
	updated, err := h.svc.UpdateStore(c.Request.Context(), s)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *Handler) GetLayout(c *gin.Context) {
	areas, aisles, bins, err := h.svc.GetLayout(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"areas": areas, "aisles": aisles, "bins": bins})
}

func (h *Handler) CreateArea(c *gin.Context) {
	var a Area
	if err := c.ShouldBindJSON(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	a.StoreID = c.Param("id")
	created, err := h.svc.CreateArea(c.Request.Context(), a)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) CreateAisle(c *gin.Context) {
	var a Aisle
	if err := c.ShouldBindJSON(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	created, err := h.svc.CreateAisle(c.Request.Context(), a)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) CreateBin(c *gin.Context) {
	var b Bin
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	created, err := h.svc.CreateBin(c.Request.Context(), b)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

// ImportLayout accepts a CSV with headers: area_code, area_name, aisle_code, aisle_name, bin_code, bin_name
func (h *Handler) ImportLayout(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file field required (multipart/form-data)"})
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid CSV: " + err.Error()})
		return
	}

	if len(records) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CSV must have a header row and at least one data row"})
		return
	}

	header := records[0]
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}

	required := []string{"area_code", "area_name", "aisle_code", "aisle_name", "bin_code", "bin_name"}
	for _, col := range required {
		if _, ok := idx[col]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("missing required column: %s", col)})
			return
		}
	}

	var rows []LayoutImportRow
	for i, record := range records[1:] {
		if len(record) == 0 {
			continue
		}
		row := LayoutImportRow{
			AreaCode:  strings.TrimSpace(record[idx["area_code"]]),
			AreaName:  strings.TrimSpace(record[idx["area_name"]]),
			AisleCode: strings.TrimSpace(record[idx["aisle_code"]]),
			AisleName: strings.TrimSpace(record[idx["aisle_name"]]),
			BinCode:   strings.TrimSpace(record[idx["bin_code"]]),
			BinName:   strings.TrimSpace(record[idx["bin_name"]]),
		}
		if row.AreaCode == "" || row.AisleCode == "" || row.BinCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("row %d: area_code, aisle_code, bin_code are required", i+2)})
			return
		}
		rows = append(rows, row)
	}

	if err := h.svc.BulkImportLayout(c.Request.Context(), c.Param("id"), rows); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"imported": len(rows)})
}

// GetAllLabels returns an SVG sheet of all bin barcodes for a store.
func (h *Handler) GetAllLabels(c *gin.Context) {
	_, _, bins, err := h.svc.GetLayout(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	svg := buildLabelSheet(bins)
	c.Data(http.StatusOK, "image/svg+xml", []byte(svg))
}

// GetBinLabel returns a single bin label as SVG.
func (h *Handler) GetBinLabel(c *gin.Context) {
	bin, err := h.svc.GetBinByBarcode(c.Request.Context(), c.Param("bin_id"))
	if err != nil {
		bin = &Bin{
			ID:      c.Param("bin_id"),
			BinCode: c.Param("bin_id"),
			BinName: "Bin",
			Barcode: c.Param("bin_id"),
		}
	}
	svg := buildSingleLabel(*bin)
	c.Data(http.StatusOK, "image/svg+xml", []byte(svg))
}

func buildLabelSheet(bins []Bin) string {
	const cols = 6
	const labelW, labelH = 160, 90
	const margin = 8
	const pageW = cols*labelW + (cols+1)*margin

	rows := (len(bins) + cols - 1) / cols
	pageH := rows*labelH + (rows+1)*margin

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`,
		pageW, pageH, pageW, pageH))
	sb.WriteString(`<style>text{font-family:monospace;font-size:11px;}` +
		`@media print{svg{width:100%;height:auto;}}</style>`)
	sb.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" fill="#f9f9f9"/>`, pageW, pageH))

	for i, bin := range bins {
		col := i % cols
		row := i / cols
		x := col*labelW + (col+1)*margin
		y := row*labelH + (row+1)*margin
		sb.WriteString(buildLabelSVGFragment(bin, x, y, labelW, labelH))
	}

	sb.WriteString(`</svg>`)
	return sb.String()
}

func buildSingleLabel(bin Bin) string {
	const w, h = 200, 120
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">%s</svg>`,
		w, h, buildLabelSVGFragment(bin, 0, 0, w, h))
}

func buildLabelSVGFragment(bin Bin, x, y, w, h int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="white" stroke="#333" stroke-width="1"/>`, x, y, w, h))
	sb.WriteString(fmt.Sprintf(`<text x="%d" y="%d" text-anchor="middle" font-size="14" font-weight="bold">%s</text>`,
		x+w/2, y+20, html.EscapeString(bin.BinCode)))
	sb.WriteString(fmt.Sprintf(`<text x="%d" y="%d" text-anchor="middle" font-size="10" fill="#666">%s</text>`,
		x+w/2, y+35, html.EscapeString(bin.BinName)))
	barX := x + 10
	for i := 0; i < 40; i++ {
		barW := 1
		if i%3 == 0 {
			barW = 2
		}
		fill := "#000"
		if i%5 == 0 {
			fill = "#fff"
		}
		sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="25" fill="%s"/>`, barX, y+42, barW, fill))
		barX += barW + 1
	}
	sb.WriteString(fmt.Sprintf(`<text x="%d" y="%d" text-anchor="middle" font-size="8">%s</text>`,
		x+w/2, y+h-8, html.EscapeString(bin.Barcode)))
	return sb.String()
}
