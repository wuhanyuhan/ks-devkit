package ksapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kstypes "github.com/wuhanyuhan/ks-types"
)

// TestReadinessWireConformance 锁定 ks-types wire 类型与 shared-fixtures golden 一致（Go 侧）。
func TestReadinessWireConformance(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "shared-fixtures", "readiness.json"))
	require.NoError(t, err)
	var f struct {
		Report      json.RawMessage `json:"readiness_report"`
		InitRequest json.RawMessage `json:"init_request"`
	}
	require.NoError(t, json.Unmarshal(raw, &f))

	var rep kstypes.ReadinessReport
	require.NoError(t, json.Unmarshal(f.Report, &rep))
	require.Len(t, rep.Gates, 2)
	assert.Equal(t, "corpus_embed", rep.Gates[0].ID)
	assert.Equal(t, kstypes.ReadinessGateStatusRunning, rep.Gates[0].Status)
	require.NotNil(t, rep.Gates[0].Progress)
	assert.Equal(t, 42, *rep.Gates[0].Progress)
	assert.Equal(t, "已嵌入 1200/2900 条", rep.Gates[0].Message)
	assert.Equal(t, kstypes.ReadinessGateStatusReady, rep.Gates[1].Status)
	assert.Nil(t, rep.Gates[1].Progress, "ready 门无 progress（omitempty）")

	var ir kstypes.ReadinessInitRequest
	require.NoError(t, json.Unmarshal(f.InitRequest, &ir))
	assert.Equal(t, "corpus_embed", ir.GateID)
}
