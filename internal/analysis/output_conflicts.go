package analysis

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output/handlers"
)

type outputRecord struct {
	target *model.Target
	output model.Output
	path   string
}

func detectOutputConflicts(graph *dag.DirectedTargetGraph) error {
	var fileOutputs []outputRecord
	var dirOutputs []outputRecord
	dockerOutputs := make(map[string][]outputRecord)

	for _, node := range graph.GetNodes() {
		target, ok := node.(*model.Target)
		if !ok {
			continue
		}

		for _, output := range target.AllOutputs() {
			switch output.Type {
			case string(handlers.DockerHandler):
				dockerOutputs[output.Identifier] = append(dockerOutputs[output.Identifier], outputRecord{
					target: target,
					output: output,
				})
			case string(handlers.DirHandler):
				dirOutputs = append(dirOutputs, outputRecord{
					target: target,
					output: output,
					path:   cleanOutputPath(target, output.Identifier),
				})
			default:
				fileOutputs = append(fileOutputs, outputRecord{
					target: target,
					output: output,
					path:   cleanOutputPath(target, output.Identifier),
				})
			}
		}
	}

	var conflicts []string
	ancestorCache := make(map[label.TargetLabel]map[label.TargetLabel]struct{})

	addConflict := func(message string) {
		conflicts = append(conflicts, "- "+message)
	}

	for tag, records := range dockerOutputs {
		for i := 0; i < len(records); i++ {
			for j := i + 1; j < len(records); j++ {
				if targetsAreOrdered(graph, records[i].target, records[j].target, ancestorCache) {
					continue
				}
				addConflict(fmt.Sprintf("%s and %s both declare docker image %q", records[i].target.Label, records[j].target.Label, tag))
			}
		}
	}

	fileMap := make(map[string][]outputRecord)
	for _, record := range fileOutputs {
		fileMap[record.path] = append(fileMap[record.path], record)
	}

	for path, records := range fileMap {
		for i := 0; i < len(records); i++ {
			for j := i + 1; j < len(records); j++ {
				if targetsAreOrdered(graph, records[i].target, records[j].target, ancestorCache) {
					continue
				}
				addConflict(fmt.Sprintf("%s and %s both write file output %q", records[i].target.Label, records[j].target.Label, path))
			}
		}
	}

	for i := 0; i < len(dirOutputs); i++ {
		for j := i + 1; j < len(dirOutputs); j++ {
			if targetsAreOrdered(graph, dirOutputs[i].target, dirOutputs[j].target, ancestorCache) {
				continue
			}
			if pathsOverlap(dirOutputs[i].path, dirOutputs[j].path) {
				addConflict(fmt.Sprintf("%s and %s both write overlapping directories (%q and %q)", dirOutputs[i].target.Label, dirOutputs[j].target.Label, dirOutputs[i].path, dirOutputs[j].path))
			}
		}
	}

	for _, dirRecord := range dirOutputs {
		for _, fileRecord := range fileOutputs {
			if targetsAreOrdered(graph, dirRecord.target, fileRecord.target, ancestorCache) {
				continue
			}
			if pathWithin(fileRecord.path, dirRecord.path) {
				addConflict(fmt.Sprintf("%s writes directory %q which overlaps file output %q from %s", dirRecord.target.Label, dirRecord.path, fileRecord.path, fileRecord.target.Label))
			}
		}
	}

	if len(conflicts) == 0 {
		return nil
	}

	sort.Strings(conflicts)

	return fmt.Errorf("conflicting outputs detected between independent targets:\n%s\nNote: These overlapping outputs create a race condition that can lead to unexpected or inconsistent build results.", strings.Join(conflicts, "\n"))
}

func cleanOutputPath(target *model.Target, output string) string {
	return filepath.Clean(filepath.Join(target.Label.Package, output))
}

func pathWithin(path, dir string) bool {
	if path == dir {
		return true
	}

	dirWithSeparator := dir + string(filepath.Separator)
	return strings.HasPrefix(path, dirWithSeparator)
}

func pathsOverlap(a, b string) bool {
	return pathWithin(a, b) || pathWithin(b, a)
}

func targetsAreOrdered(graph *dag.DirectedTargetGraph, a, b model.BuildNode, ancestorCache map[label.TargetLabel]map[label.TargetLabel]struct{}) bool {
	if ancestorCache == nil {
		ancestorCache = make(map[label.TargetLabel]map[label.TargetLabel]struct{})
	}

	ancestorsOfA := getAncestorSet(graph, a, ancestorCache)
	if _, ok := ancestorsOfA[b.GetLabel()]; ok {
		return true
	}

	ancestorsOfB := getAncestorSet(graph, b, ancestorCache)
	if _, ok := ancestorsOfB[a.GetLabel()]; ok {
		return true
	}

	return false
}

func getAncestorSet(graph *dag.DirectedTargetGraph, node model.BuildNode, cache map[label.TargetLabel]map[label.TargetLabel]struct{}) map[label.TargetLabel]struct{} {
	if ancestors, ok := cache[node.GetLabel()]; ok {
		return ancestors
	}

	set := make(map[label.TargetLabel]struct{})
	stack := append([]model.BuildNode{}, graph.GetDependencies(node)...)

	for len(stack) > 0 {
		ancestor := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if _, seen := set[ancestor.GetLabel()]; seen {
			continue
		}

		set[ancestor.GetLabel()] = struct{}{}

		if cached, ok := cache[ancestor.GetLabel()]; ok {
			for cachedAncestor := range cached {
				set[cachedAncestor] = struct{}{}
			}
			continue
		}

		stack = append(stack, graph.GetDependencies(ancestor)...)
	}

	cache[node.GetLabel()] = set

	return set
}
