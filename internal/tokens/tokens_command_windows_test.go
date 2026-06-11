//go:build windows

package tokens

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCreateNoWindow = 0x08000000

func TestBuildAgentsviewCmdHidesConsole(t *testing.T) {
	cmd := buildAgentsviewCmd(context.Background(), "agentsview", "--version")
	require.NotNil(t, cmd.SysProcAttr)
	assert.NotZero(t, cmd.SysProcAttr.CreationFlags&testCreateNoWindow)
}
