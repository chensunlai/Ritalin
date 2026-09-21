package ritalin

import (
	"context"
	"errors"
)

func confirmedNode(c *Config, id string) *Node {
	for i := range c.Nodes {
		n := &c.Nodes[i]
		if id != "" && n.ID == id && n.Checked != "" && len(n.Reach) > 0 {
			return n
		}
	}
	return nil
}

func useNode(c *Config, state *State) *Node {
	id := state.UseNodeID
	if id == "" {
		id = state.NodeID
	}
	// Never infer a binding from a display name supplied by an imported file.
	return confirmedNode(c, id)
}

// The selected node replaces, rather than chains through, the default upstream.
// Its lifetime is the same as the Codex process; failures never fall back.
func startUseWarp(ctx context.Context, s *Store, c Config, state *State, emit Emit) (*Warp, func(), error) {
	upstream := c.Upstream
	var run *ClashRun
	if c.UseNode {
		n := useNode(&c, state)
		if n == nil {
			return nil, nil, errors.New(uiText(c.Language, "请在使用页为此状态选择检测通过的节点"))
		}
		var err error
		run, err = startClash(ctx, s, c, []Node{*n}, emit)
		if err != nil {
			return nil, nil, err
		}
		upstream = run.URLs[n.ID]
		if upstream == "" {
			run.Close()
			return nil, nil, errors.New(uiText(c.Language, "节点没有可用的代理地址"))
		}
	}
	w, err := startWarp(s, state.Value, c.Replace, upstream)
	if err != nil {
		run.Close()
		return nil, nil, err
	}
	return w, func() { w.Close(); run.Close() }, nil
}

func (m *ui) pickUseNode(id string) {
	for _, n := range m.c.Nodes {
		if confirmedNode(&m.c, n.ID) != nil {
			m.routeState, m.routeCursor = id, m.cursor
			m.cursor, m.bodyOffset = 0, 0
			m.details = false
			return
		}
	}
	m.notice = m.t("请先在代理页添加并检测节点")
}

func (m *ui) closeNodePicker() {
	m.routeState = ""
	m.cursor = m.routeCursor
	m.details = false
}
