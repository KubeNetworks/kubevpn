package cmds

import (
	"net/http"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/wencaiwulue/kubevpn/pkg/config"
	"github.com/wencaiwulue/kubevpn/pkg/handler"
	"github.com/wencaiwulue/kubevpn/pkg/util"
)

var route handler.Route

func init() {
	ServerCmd.Flags().StringArrayVarP(&route.ServeNodes, "nodeCommand", "L", []string{}, "command needs to be executed")
	ServerCmd.Flags().StringVarP(&route.ChainNode, "chainCommand", "F", "", "command needs to be executed")
	ServerCmd.Flags().BoolVar(&config.Debug, "debug", false, "true/false")
	RootCmd.AddCommand(ServerCmd)
}

var ServerCmd = &cobra.Command{
	Use:   "serve",
	Short: "serve receive traffic and redirect to traffic manager",
	Long:  `serve receive traffic and redirect to traffic manager`,
	PreRun: func(*cobra.Command, []string) {
		util.InitLogger(config.Debug)
		go func() { log.Info(http.ListenAndServe("localhost:6060", nil)) }()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		err := handler.StartTunServer(cmd.Context(), route)
		if err != nil {
			return err
		}
		<-cmd.Context().Done()
		return nil
	},
}
