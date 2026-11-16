package example

import (
	"fmt"

	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// CommandPing is the example plugin's reference CLI subcommand name.
const CommandPing = "example:ping"

// registerCLICommands registers CommandPing, closing over feeMinorUnits so
// the command reflects live (admin-updated) plugin state.
func registerCLICommands(sdk *pluginsdk.SDK, feeMinorUnits *int64) {
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        CommandPing,
		Description: "Verify example plugin CLI registration",
		Run: func(ctx pluginsdk.CLIContext, _ []string) error {
			fmt.Println("example plugin ok")
			if ctx.Logger != nil {
				fee := int64(defaultExampleFeeMinorUnits)
				if feeMinorUnits != nil && *feeMinorUnits > 0 {
					fee = *feeMinorUnits
				}
				ctx.Logger.Info("example.ping", map[string]interface{}{
					"fee_minor_units": fee,
				})
			}
			return nil
		},
	})
}
