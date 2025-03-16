package analysis

import (
	"fmt"
	"grog/pkg/model"
)

func BuildGraphAndAnalyze(targets []model.Target) (model.Graph, error) {
	g, err := buildGraph(targets)
	if err != nil {
		return model.Graph{}, err
	}
	return g, nil
}

func buildGraph(targets []model.Target) (model.Graph, error) {
	g := model.Graph{
		Nodes: make(map[string]model.Target),
	}
	for _, t := range targets {
		if _, exists := g.Nodes[t.Label.String()]; exists {
			return model.Graph{}, fmt.Errorf("duplicate target name: %s", t.Name)
		}
		g.Nodes[t.Name] = t
	}
	for _, t := range targets {
		for _, dep := range t.Deps {
			if _, exists := g.Nodes[dep]; !exists {
				return model.Graph{}, fmt.Errorf("target %s references unresolved dependency: %s", t.Name, dep)
			}
		}
	}
	return g, nil
}
