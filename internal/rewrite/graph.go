package rewrite

import (
	"fmt"
	"sort"
)

// topoSort returns a topological order of vertices given edges (dependency ->
// dependent). The tie-break among ready vertices is topoTieBreak, isolated so
// it can be calibrated against Java's JGraphT iteration order (risk #1).
func topoSort(vertices []string, edges [][2]string) ([]string, error) {
	inDeg := map[string]int{}
	adj := map[string][]string{}
	for _, v := range vertices {
		inDeg[v] = 0
	}
	for _, e := range edges {
		from, to := e[0], e[1]
		adj[from] = append(adj[from], to)
		inDeg[to]++
	}
	var ready []string
	for v, d := range inDeg {
		if d == 0 {
			ready = append(ready, v)
		}
	}
	topoTieBreak(ready)
	var order []string
	for len(ready) > 0 {
		v := ready[0]
		ready = ready[1:]
		order = append(order, v)
		next := append([]string(nil), adj[v]...)
		sort.Strings(next) // deterministic edge processing
		for _, w := range next {
			inDeg[w]--
			if inDeg[w] == 0 {
				ready = append(ready, w)
				topoTieBreak(ready)
			}
		}
	}
	if len(order) != len(inDeg) {
		return nil, fmt.Errorf("found cycle in models")
	}
	return order, nil
}

// topoTieBreak orders ready vertices. CALIBRATION POINT for risk #1: the
// initial guess is lexical order; task 21 verifies against multi-model golden.
func topoTieBreak(ready []string) {
	sort.Strings(ready)
}
