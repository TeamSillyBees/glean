package catalog

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCommittedBaselineCatalogCompatibility 验证全部真实快照能被目录导入 DTO 接受，避免只测抽样后遗漏类型或日期来源问题。
func TestCommittedBaselineCatalogCompatibility(t *testing.T) {
	directory := filepath.Join("..", "..", "..", "data", "baselines")
	manifestBytes, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	require.NoError(t, err)
	var manifest struct {
		Memberships int `json:"memberships"`
		Shards      []struct {
			File    string `json:"file"`
			Topic   string `json:"topic"`
			Records int    `json:"records"`
		} `json:"shards"`
	}
	require.NoError(t, json.Unmarshal(manifestBytes, &manifest))
	counts := map[string]int{}
	all := 0
	for _, shard := range manifest.Shards {
		t.Run(shard.File, func(t *testing.T) {
			require.Equal(t, filepath.Base(shard.File), shard.File)
			file, err := os.Open(filepath.Join(directory, shard.File))
			require.NoError(t, err)
			defer file.Close()
			compressed, err := gzip.NewReader(file)
			require.NoError(t, err)
			defer compressed.Close()
			decoder := json.NewDecoder(compressed)
			count := 0
			for {
				var record ImportRecord
				err := decoder.Decode(&record)
				if err == io.EOF {
					break
				}
				require.NoError(t, err, "line %d", count+1)
				require.Equal(t, shard.Topic, record.Topic)
				require.NoError(t, normalizeImport(&record), "originalId=%s", record.OriginalID)
				require.NotEmpty(t, record.Raw)
				require.False(t, record.IsMock)
				counts[record.Topic]++
				count++
			}
			require.Equal(t, shard.Records, count)
			all += count
		})
	}
	require.Equal(t, 39495, all)
	require.Equal(t, manifest.Memberships, all)
	require.Equal(t, map[string]int{"biomed-ai": 31399, "llm-agent": 4059, "biomed-worldmodel": 4037}, counts)
}
