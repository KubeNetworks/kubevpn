package cmd

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/wencaiwulue/kubevpn/pkg"
	"github.com/wencaiwulue/kubevpn/util"
)

var config pkg.Route

func init() {
	ServerCmd.Flags().StringArrayVarP(&config.ServeNodes, "nodeCommand", "L", []string{}, "command needs to be executed")
	ServerCmd.Flags().StringVarP(&config.ChainNode, "chainCommand", "F", "", "command needs to be executed")
	ServerCmd.Flags().BoolVar(&util.Debug, "debug", false, "true/false")
	RootCmd.AddCommand(ServerCmd)
}

var ServerCmd = &cobra.Command{
	Use:   "serve",
	Short: "serve",
	Long:  `serve`,
	PreRun: func(*cobra.Command, []string) {
		util.InitLogger(util.Debug)
	},
	Run: func(cmd *cobra.Command, args []string) {
		c, err := pkg.Start(config)
		if err != nil {
			log.Fatal(err)
		}
		if err := <-c; err != nil {
			log.Fatal(err)
		}
	},
}
