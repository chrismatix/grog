package cmd

import (
	"os"
	"os/exec"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestConfigureRootWithoutHome(t *testing.T) {
	if os.Getenv("GROG_TEST_WITHOUT_HOME") == "1" {
		require.Equal(t, os.Getenv("GROG_ROOT"), viper.GetString("root"))
		RootCmd.SetArgs([]string{"--help"})
		require.NoError(t, RootCmd.Execute())
		return
	}

	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("GROG_ROOT", t.TempDir())
	t.Setenv("GROG_TEST_WITHOUT_HOME", "1")
	executable, err := os.Executable()
	require.NoError(t, err)
	command := exec.Command(executable, "-test.run=^TestConfigureRootWithoutHome$")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
