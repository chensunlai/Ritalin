package ritalin

import (
	"context"
	"errors"
	"fmt"
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

func testNode(c *Config, state *State) *Node {
	if n := confirmedNode(c, state.NodeID); n != nil {
		return n
	}
	return useNode(c, state)
}

func startTestWarp(ctx context.Context, s *Store, c Config, state *State, emit Emit) (*Warp, func(), error) {
	n := testNode(&c, state)
	if n == nil {
		return nil, nil, errors.New(uiText(c.Language, "请先为此候选选择检测通过的节点"))
	}
	// Tests use the candidate's source (or its explicit imported binding),
	// independently of the active state's connection mode and replacement toggle.
	c.UseNode, c.Replace = true, true
	c.UseForceState = c.TestForceState
	candidate := *state
	candidate.UseNodeID = n.ID
	emit(fmt.Sprintf(uiText(c.Language, "测试节点：%s"), safeText(n.Name)))
	return startUseWarpObserved(ctx, s, c, &candidate, emit, func(event warpStateEvent) {
		emit(event.text(c.Language))
	})
}

// The selected node replaces, rather than chains through, the default upstream.
// Its lifetime is the same as the Codex process; failures never fall back.
func startUseWarp(ctx context.Context, s *Store, c Config, state *State, emit Emit) (*Warp, func(), error) {
	return startUseWarpObserved(ctx, s, c, state, emit, nil)
}

func startUseWarpObserved(ctx context.Context, s *Store, c Config, state *State, emit Emit, observe func(warpStateEvent)) (*Warp, func(), error) {
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
	w, err := startWarpObserved(s, state.Value, c.Replace, c.UseForceState, upstream, observe)
	if err != nil {
		run.Close()
		return nil, nil, err
	}
	return w, func() { w.Close(); run.Close() }, nil
}

func (m *ui) pickUseNode(id string) {
	m.routeTest = false
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

func (m *ui) pickTestNode(id string) {
	m.pickUseNode(id)
	m.routeTest = m.routeState != ""
	if !m.routeTest {
		m.batch = false
	}
}

func (m *ui) closeNodePicker() {
	if m.routeTest {
		m.batch = false
	}
	m.routeTest = false
	m.routeState = ""
	m.cursor = m.routeCursor
	m.details = false
}
