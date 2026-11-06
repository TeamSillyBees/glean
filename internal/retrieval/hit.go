package retrieval

import (
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"time"
)

// Hit 记录来源观察被运行及查询发现的不可变路径，不参与论文身份或来源内容散列。
type Hit struct {
	RunID        idgen.ID  `json:"runId"`
	QueryID      string    `json:"queryId"`
	QueryVersion string    `json:"queryVersion"`
	PlanVersion  string    `json:"planVersion"`
	Source       string    `json:"source"`
	Query        string    `json:"query"`
	Mode         string    `json:"mode"`
	DiscoveredAt time.Time `json:"discoveredAt"`
}
