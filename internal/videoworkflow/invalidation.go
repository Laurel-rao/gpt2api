package videoworkflow

import "sort"

type ChangeKind string

const (
	ChangeSemantic      ChangeKind = "semantic"
	ChangeRoleSelection ChangeKind = "role_selection"
	ChangeBackground    ChangeKind = "background"
	ChangeTimelineOrder ChangeKind = "timeline_order"
	ChangeLayout        ChangeKind = "layout"
)

// InvalidatedNodeIDs 返回一次修改之后必须重新计算的后代节点，不包含修改源节点。
func InvalidatedNodeIDs(g Graph, changedNodeID string, kind ChangeKind) []string {
	if kind == ChangeLayout {
		return nil
	}
	nodes := make(map[string]Node, len(g.Nodes))
	for _, node := range g.Nodes {
		nodes[node.ID] = node
	}
	if _, ok := nodes[changedNodeID]; !ok {
		return nil
	}
	adj := make(map[string][]string)
	for _, edge := range g.Edges {
		if _, sourceOK := nodes[edge.Source]; !sourceOK {
			continue
		}
		if _, targetOK := nodes[edge.Target]; !targetOK {
			continue
		}
		adj[edge.Source] = append(adj[edge.Source], edge.Target)
	}
	seen := map[string]bool{changedNodeID: true}
	queue := []string{changedNodeID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range adj[current] {
			if seen[next] {
				continue
			}
			seen[next] = true
			queue = append(queue, next)
		}
	}
	delete(seen, changedNodeID)

	// 时间线排序只改变最终拼接，错误连线也不能扩大失效范围。
	if kind == ChangeTimelineOrder {
		for id := range seen {
			if nodes[id].Type != NodeCompose {
				delete(seen, id)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
