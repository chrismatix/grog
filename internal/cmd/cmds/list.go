package cmds

//
//var ListCmd = &cobra.Command{
//	Use:   "list",
//	Short: "Lists all targets in the workspace, optionally filtered by a target pattern.",
//	Args:  cobra.ArbitraryArgs, // Optional argument for target pattern
//	Run: func(cmd *cobra.Command, args []string) {
//		ctx, logger := setupCommand()
//
//		currentPackagePath, err := config.Global.GetCurrentPackage()
//		if err != nil {
//			logger.Fatalf("could not get current package: %v", err)
//		}
//
//		targetPatterns, err := label.ParsePatternsOrMatchAll(currentPackagePath, args)
//		if err != nil {
//			logger.Fatalf("could not parse target pattern: %v", err)
//		}
//
//		graph := mustLoadGraph(ctx, logger)
//
//	},
//}
