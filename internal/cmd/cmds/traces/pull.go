package traces

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"grog/internal/console"
	"grog/internal/tracing"
)

var pullCmd = &cobra.Command{
	Use:     "pull",
	Short:   "Download remote traces to local cache for querying.",
	Example: `  grog traces pull`,
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()
		store := getStore(ctx, logger)
		defer store.Close()

		var onProgress tracing.PullProgress
		var teardown func()

		if console.UseTea() {
			teaCtx, program, sendMsg := console.StartTaskUI(ctx)
			ctx = teaCtx

			startedAt := time.Now().Unix()

			onProgress = func(current, total int) {
				sendMsg(console.TaskStateMsg{
					State: console.TaskStateMap{
						0: console.TaskState{
							Status:       fmt.Sprintf("Pulling traces (%d/%d)", current, total),
							StartedAtSec: startedAt,
							Progress: &console.Progress{
								StartedAtSec: startedAt,
								Current:      int64(current),
								Total:        int64(total),
								Unit:         console.ProgressUnitCount,
							},
						},
					},
				})
			}
			teardown = func() {
				program.Quit()
				_ = program.ReleaseTerminal()
			}
		}

		pulled, err := store.Pull(ctx, onProgress)

		if teardown != nil {
			teardown()
		}

		if err != nil {
			logger.Fatalf("pull failed: %v", err)
		}
		logger.Infof("Pulled %d remote trace files.", pulled)
	},
}

func registerPullCmd() {
	Cmd.AddCommand(pullCmd)
}
