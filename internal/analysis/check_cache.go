package analysis

// CheckCacheForSelected checks whether the Inputs of the selected targets have changed.
// and sets the InputsChanged flag on the targets.
// Returns the number of targets that have changed.
//func CheckCacheForSelected(ctx context.Context, graph *dag.DirectedTargetGraph) error {
//	numWorkers := viper.GetInt("num_workers")
//	logger := console.GetLogger(ctx)
//
//	p, msgCh := console.StartTaskUI(ctx)
//	defer func(p *tea.Program) {
//		err := p.ReleaseTerminal()
//		if err != nil {
//			logger.Errorf("error releasing terminal: %v", err)
//		}
//	}(p)
//	defer p.Quit()
//
//	workerPool := worker.NewPool(numWorkers, msgCh)
//	workerPool.StartWorkers(ctx)
//
//	selectedTasks := graph.GetSelectedVertices()
//	tasks := []worker.TaskFunc{}
//	// Loop over all selected targets
//	for _, target := range selectedTasks {
//		taskFunc := func(update worker.StatusFunc, log worker.LogFunc) error {
//
//		}
//	}
//
//	return workerPool.RunAll(tasks)
//}
