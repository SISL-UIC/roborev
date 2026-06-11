//go:build windows

package kata

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCreateNoWindow = 0x08000000

func TestBuildKataCmdHidesConsole(t *testing.T) {
	c := &CLIClient{bin: "kata", workdir: t.TempDir()}
	cmd := buildKataCmd(context.Background(), c, []string{"list"}, strings.NewReader(""))
	require.NotNil(t, cmd.SysProcAttr)
	assert.NotZero(t, cmd.SysProcAttr.CreationFlags&testCreateNoWindow)
}
