package retrieval

import "github.com/teamsillybees/glean/internal/platform/idgen"

// CatalogExportRecord 是目录提供给检索审计的最小共享契约，不包含摘要或全文正文。
type CatalogExportRecord struct {
	ID            idgen.ID         `json:"record_id"`
	Record        map[string]any   `json:"record"`
	Screening     []map[string]any `json:"screening"`
	IdentityEdges []map[string]any `json:"identity_edges"`
}
