//go:build windows

package github

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCreateNoWindow = 0x08000000

func TestBuildGhAuthCmdHidesConsole(t *testing.T) {
	cmd := buildGhAuthCmd(context.Background(), []string{"auth", "token"})
	require.NotNil(t, cmd.SysProcAttr)
	assert.NotZero(t, cmd.SysProcAttr.CreationFlags&testCreateNoWindow)
}
