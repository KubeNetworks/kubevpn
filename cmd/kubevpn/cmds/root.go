package cmds

import "github.com/spf13/cobra"

var RootCmd = &cobra.Command{
	Use:          "kubevpn",
	Short:        "kubevpn",
	Long:         `kubevpn`,
	SilenceUsage: true,
}
