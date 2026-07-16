package service

import (
	"math/bits"
	"slices"
	"sync"
	"sync/atomic"
)

const cyberPreflightRuleGroupCount = 8

const (
	cyberPreflightRuleGroupStandaloneBlock = iota
	cyberPreflightRuleGroupHard
	cyberPreflightRuleGroupOffensiveIntent
	cyberPreflightRuleGroupCredentialAbuseIntent
	cyberPreflightRuleGroupTechnique
	cyberPreflightRuleGroupCredential
	cyberPreflightRuleGroupTarget
	cyberPreflightRuleGroupDefensive
)

type cyberPreflightRuleMatches struct {
	StandaloneBlock       string
	Hard                  string
	OffensiveIntent       string
	CredentialAbuseIntent string
	Technique             string
	Credential            string
	Target                string
	Defensive             string
}

type cyberPreflightRuleMatcher struct {
	nodes           []cyberPreflightRuleNode
	edges           []cyberPreflightRuleEdge
	rootTransitions [256]int32
	phrases         [cyberPreflightRuleGroupCount][]string
}

type cyberPreflightRuleNode struct {
	failure   int32
	bestMatch [cyberPreflightRuleGroupCount]int32
	matchMask uint8
	edgeStart uint32
	edgeCount uint16
}

type cyberPreflightRuleEdge struct {
	target int32
	label  byte
}

type cyberPreflightRuleBuildEdge struct {
	target      int32
	nextSibling int32
	label       byte
}

type cyberPreflightRuleMatcherCacheEntry struct {
	sourceRules ContentModerationCyberPreflightRulesConfig
	matcher     *cyberPreflightRuleMatcher
}

var (
	cyberPreflightRuleMatcherCache   atomic.Pointer[cyberPreflightRuleMatcherCacheEntry]
	cyberPreflightRuleMatcherCacheMu sync.Mutex
)

func loadCyberPreflightRuleMatcher(rules ContentModerationCyberPreflightRulesConfig) *cyberPreflightRuleMatcher {
	if entry := cyberPreflightRuleMatcherCache.Load(); entry != nil && equalCyberPreflightRules(entry.sourceRules, rules) {
		return entry.matcher
	}

	cyberPreflightRuleMatcherCacheMu.Lock()
	defer cyberPreflightRuleMatcherCacheMu.Unlock()
	if entry := cyberPreflightRuleMatcherCache.Load(); entry != nil && equalCyberPreflightRules(entry.sourceRules, rules) {
		return entry.matcher
	}

	sourceRules := rules.clone()
	rules.normalize()
	matcher := newCyberPreflightRuleMatcher(rules)
	cyberPreflightRuleMatcherCache.Store(&cyberPreflightRuleMatcherCacheEntry{
		sourceRules: sourceRules,
		matcher:     matcher,
	})
	return matcher
}

func equalCyberPreflightRules(left, right ContentModerationCyberPreflightRulesConfig) bool {
	return slices.Equal(left.StandaloneBlockMarkers, right.StandaloneBlockMarkers) &&
		slices.Equal(left.HardMarkers, right.HardMarkers) &&
		slices.Equal(left.OffensiveIntentMarkers, right.OffensiveIntentMarkers) &&
		slices.Equal(left.CredentialAbuseIntentMarkers, right.CredentialAbuseIntentMarkers) &&
		slices.Equal(left.TechniqueMarkers, right.TechniqueMarkers) &&
		slices.Equal(left.CredentialMarkers, right.CredentialMarkers) &&
		slices.Equal(left.TargetMarkers, right.TargetMarkers) &&
		slices.Equal(left.DefensiveMarkers, right.DefensiveMarkers)
}

func newCyberPreflightRuleMatcher(rules ContentModerationCyberPreflightRulesConfig) *cyberPreflightRuleMatcher {
	phraseGroups := [cyberPreflightRuleGroupCount][]string{
		rules.StandaloneBlockMarkers,
		rules.HardMarkers,
		rules.OffensiveIntentMarkers,
		rules.CredentialAbuseIntentMarkers,
		rules.TechniqueMarkers,
		rules.CredentialMarkers,
		rules.TargetMarkers,
		rules.DefensiveMarkers,
	}
	matcher := &cyberPreflightRuleMatcher{
		nodes: []cyberPreflightRuleNode{newCyberPreflightRuleNode()},
	}
	buildEdges := make([]cyberPreflightRuleBuildEdge, 0)

	for groupIndex, phrases := range phraseGroups {
		matcher.phrases[groupIndex] = cloneStrings(phrases)
		for phraseIndex, phrase := range phrases {
			if phrase == "" {
				continue
			}
			state := int32(0)
			for _, label := range []byte(phrase) {
				next := cyberPreflightRuleBuildTransition(matcher.nodes, buildEdges, state, label)
				if next < 0 {
					next = int32(len(matcher.nodes))
					matcher.nodes = append(matcher.nodes, newCyberPreflightRuleNode())
					buildEdges = append(buildEdges, cyberPreflightRuleBuildEdge{
						target:      next,
						nextSibling: cyberPreflightRuleBuildFirstEdge(matcher.nodes[state]),
						label:       label,
					})
					matcher.nodes[state].edgeStart = uint32(len(buildEdges))
				}
				state = next
			}
			current := matcher.nodes[state].bestMatch[groupIndex]
			if current < 0 || int32(phraseIndex) < current {
				matcher.nodes[state].bestMatch[groupIndex] = int32(phraseIndex)
				matcher.nodes[state].matchMask |= 1 << groupIndex
			}
		}
	}

	matcher.buildFailureTransitions(buildEdges)
	matcher.flattenEdges(buildEdges)
	return matcher
}

func newCyberPreflightRuleNode() cyberPreflightRuleNode {
	node := cyberPreflightRuleNode{}
	for groupIndex := range node.bestMatch {
		node.bestMatch[groupIndex] = -1
	}
	return node
}

func cyberPreflightRuleBuildFirstEdge(node cyberPreflightRuleNode) int32 {
	if node.edgeStart == 0 {
		return -1
	}
	return int32(node.edgeStart - 1)
}

func cyberPreflightRuleBuildTransition(nodes []cyberPreflightRuleNode, edges []cyberPreflightRuleBuildEdge, state int32, label byte) int32 {
	for edgeIndex := cyberPreflightRuleBuildFirstEdge(nodes[state]); edgeIndex >= 0; edgeIndex = edges[edgeIndex].nextSibling {
		if edges[edgeIndex].label == label {
			return edges[edgeIndex].target
		}
	}
	return -1
}

func (m *cyberPreflightRuleMatcher) buildFailureTransitions(buildEdges []cyberPreflightRuleBuildEdge) {
	queue := make([]int32, 0, len(m.nodes)-1)
	for edgeIndex := cyberPreflightRuleBuildFirstEdge(m.nodes[0]); edgeIndex >= 0; edgeIndex = buildEdges[edgeIndex].nextSibling {
		edge := buildEdges[edgeIndex]
		m.rootTransitions[edge.label] = edge.target
		queue = append(queue, edge.target)
	}

	for queueIndex := 0; queueIndex < len(queue); queueIndex++ {
		state := queue[queueIndex]
		for edgeIndex := cyberPreflightRuleBuildFirstEdge(m.nodes[state]); edgeIndex >= 0; edgeIndex = buildEdges[edgeIndex].nextSibling {
			edge := buildEdges[edgeIndex]
			failure := m.nodes[state].failure
			fallback := cyberPreflightRuleBuildTransition(m.nodes, buildEdges, failure, edge.label)
			for fallback < 0 && failure != 0 {
				failure = m.nodes[failure].failure
				fallback = cyberPreflightRuleBuildTransition(m.nodes, buildEdges, failure, edge.label)
			}
			if fallback >= 0 {
				m.nodes[edge.target].failure = fallback
			}
			failureNode := m.nodes[m.nodes[edge.target].failure]
			for groupIndex := range m.nodes[edge.target].bestMatch {
				m.nodes[edge.target].bestMatch[groupIndex] = minCyberPreflightRuleIndex(
					m.nodes[edge.target].bestMatch[groupIndex],
					failureNode.bestMatch[groupIndex],
				)
			}
			m.nodes[edge.target].matchMask |= failureNode.matchMask
			queue = append(queue, edge.target)
		}
	}
}

func (m *cyberPreflightRuleMatcher) flattenEdges(buildEdges []cyberPreflightRuleBuildEdge) {
	edges := make([]cyberPreflightRuleEdge, 0, len(buildEdges))
	var outgoing [256]cyberPreflightRuleEdge
	for nodeIndex := range m.nodes {
		count := 0
		for edgeIndex := cyberPreflightRuleBuildFirstEdge(m.nodes[nodeIndex]); edgeIndex >= 0; edgeIndex = buildEdges[edgeIndex].nextSibling {
			edge := buildEdges[edgeIndex]
			outgoing[count] = cyberPreflightRuleEdge{target: edge.target, label: edge.label}
			count++
		}
		for index := 1; index < count; index++ {
			current := outgoing[index]
			insertAt := index
			for insertAt > 0 && current.label < outgoing[insertAt-1].label {
				outgoing[insertAt] = outgoing[insertAt-1]
				insertAt--
			}
			outgoing[insertAt] = current
		}
		m.nodes[nodeIndex].edgeStart = uint32(len(edges))
		m.nodes[nodeIndex].edgeCount = uint16(count)
		edges = append(edges, outgoing[:count]...)
	}
	m.edges = edges
}

func minCyberPreflightRuleIndex(left, right int32) int32 {
	if left < 0 {
		return right
	}
	if right < 0 || left < right {
		return left
	}
	return right
}

func (m *cyberPreflightRuleMatcher) Match(text string) cyberPreflightRuleMatches {
	bestMatches := [cyberPreflightRuleGroupCount]int32{}
	for groupIndex := range bestMatches {
		bestMatches[groupIndex] = -1
	}

	state := int32(0)
	for index := 0; index < len(text); index++ {
		label := text[index]
		for {
			next := m.next(state, label)
			if next != 0 {
				state = next
				break
			}
			if state == 0 {
				break
			}
			state = m.nodes[state].failure
		}
		node := m.nodes[state]
		for matchMask := node.matchMask; matchMask != 0; matchMask &= matchMask - 1 {
			groupIndex := bits.TrailingZeros8(matchMask)
			bestMatches[groupIndex] = minCyberPreflightRuleIndex(bestMatches[groupIndex], node.bestMatch[groupIndex])
		}
	}

	return cyberPreflightRuleMatches{
		StandaloneBlock:       m.matchPhrase(cyberPreflightRuleGroupStandaloneBlock, bestMatches[cyberPreflightRuleGroupStandaloneBlock]),
		Hard:                  m.matchPhrase(cyberPreflightRuleGroupHard, bestMatches[cyberPreflightRuleGroupHard]),
		OffensiveIntent:       m.matchPhrase(cyberPreflightRuleGroupOffensiveIntent, bestMatches[cyberPreflightRuleGroupOffensiveIntent]),
		CredentialAbuseIntent: m.matchPhrase(cyberPreflightRuleGroupCredentialAbuseIntent, bestMatches[cyberPreflightRuleGroupCredentialAbuseIntent]),
		Technique:             m.matchPhrase(cyberPreflightRuleGroupTechnique, bestMatches[cyberPreflightRuleGroupTechnique]),
		Credential:            m.matchPhrase(cyberPreflightRuleGroupCredential, bestMatches[cyberPreflightRuleGroupCredential]),
		Target:                m.matchPhrase(cyberPreflightRuleGroupTarget, bestMatches[cyberPreflightRuleGroupTarget]),
		Defensive:             m.matchPhrase(cyberPreflightRuleGroupDefensive, bestMatches[cyberPreflightRuleGroupDefensive]),
	}
}

func (m *cyberPreflightRuleMatcher) next(state int32, label byte) int32 {
	if state == 0 {
		return m.rootTransitions[label]
	}
	node := m.nodes[state]
	left := int(node.edgeStart)
	right := left + int(node.edgeCount)
	for left < right {
		middle := left + (right-left)/2
		edge := m.edges[middle]
		if edge.label < label {
			left = middle + 1
			continue
		}
		right = middle
	}
	end := int(node.edgeStart) + int(node.edgeCount)
	if left < end && m.edges[left].label == label {
		return m.edges[left].target
	}
	return 0
}

func (m *cyberPreflightRuleMatcher) matchPhrase(groupIndex int, phraseIndex int32) string {
	if phraseIndex < 0 {
		return ""
	}
	return m.phrases[groupIndex][phraseIndex]
}
