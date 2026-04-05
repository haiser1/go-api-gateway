package gateway

import (
	"strings"
)

// radixNode represents a node in the radix tree.
// Each node can hold handlers keyed by HTTP method.
type radixNode struct {
	// path is this node's segment of the route (e.g. "/api", "/users")
	path string

	// children holds static child nodes keyed by first character of their path
	children map[byte]*radixNode

	// paramChild is a single wildcard/parameter child (e.g. :id)
	paramChild *radixNode
	paramName  string

	// handlers maps HTTP method -> ResolvedRoute for this node
	handlers map[string]*ResolvedRoute
}

// RadixTree is a radix tree for fast HTTP route lookup.
type RadixTree struct {
	root *radixNode
}

// NewRadixTree creates a new empty radix tree.
func NewRadixTree() *RadixTree {
	return &RadixTree{
		root: &radixNode{
			children: make(map[byte]*radixNode),
		},
	}
}

// Insert adds a route to the radix tree.
// Path format: "/api/users/:id/posts"
func (t *RadixTree) Insert(method, path string, route *ResolvedRoute) {
	segments := splitPath(path)
	current := t.root

	for _, seg := range segments {
		if strings.HasPrefix(seg, ":") {
			// Parameter segment
			paramName := seg[1:]
			if current.paramChild == nil {
				current.paramChild = &radixNode{
					path:      seg,
					children:  make(map[byte]*radixNode),
					paramName: paramName,
				}
			}
			current = current.paramChild
		} else {
			// Static segment — try to find a matching child
			child := current.findStaticChild(seg)
			if child != nil {
				// Check if we need to split the existing node
				commonLen := longestCommonPrefix(child.path, seg)

				if commonLen < len(child.path) {
					// Split the existing child
					splitChild := &radixNode{
						path:       child.path[commonLen:],
						children:   child.children,
						paramChild: child.paramChild,
						paramName:  child.paramName,
						handlers:   child.handlers,
					}

					child.path = child.path[:commonLen]
					child.children = make(map[byte]*radixNode)
					child.children[splitChild.path[0]] = splitChild
					child.paramChild = nil
					child.paramName = ""
					child.handlers = nil
				}

				if commonLen < len(seg) {
					// We still have remaining segment to insert
					remaining := seg[commonLen:]
					nextChild := child.children[remaining[0]]
					if nextChild == nil {
						nextChild = &radixNode{
							path:     remaining,
							children: make(map[byte]*radixNode),
						}
						child.children[remaining[0]] = nextChild
					}
					current = nextChild
				} else {
					current = child
				}
			} else {
				// No matching child at all — create new
				newNode := &radixNode{
					path:     seg,
					children: make(map[byte]*radixNode),
				}
				current.children[seg[0]] = newNode
				current = newNode
			}
		}
	}

	// Set handler on the final node
	if current.handlers == nil {
		current.handlers = make(map[string]*ResolvedRoute)
	}
	current.handlers[method] = route
}

// Search looks up a route by method and path.
// Returns the matched route and extracted path parameters.
func (t *RadixTree) Search(method, path string) (*ResolvedRoute, map[string]string) {
	segments := splitPath(path)
	params := make(map[string]string)

	route := t.search(t.root, segments, 0, method, params)
	if route != nil {
		return route, params
	}
	return nil, nil
}

// search recursively walks the tree to find a matching route.
// Static children are tried first for priority over param children.
func (t *RadixTree) search(node *radixNode, segments []string, idx int, method string, params map[string]string) *ResolvedRoute {
	// Base case: we've consumed all segments
	if idx == len(segments) {
		if node.handlers != nil {
			if route, ok := node.handlers[method]; ok {
				return route
			}
		}
		return nil
	}

	seg := segments[idx]

	// 1. Try static children first (higher priority)
	if child := node.findStaticChildForSearch(seg); child != nil {
		// The child's path may be a prefix-compressed path
		if child.path == seg {
			// Exact match on this segment
			if result := t.search(child, segments, idx+1, method, params); result != nil {
				return result
			}
		} else if strings.HasPrefix(seg, child.path) {
			// The child's path is a prefix of our segment — need to continue matching
			remaining := seg[len(child.path):]
			// Look for a continuation in child's children
			if result := t.searchCompressed(child, remaining, segments, idx, method, params); result != nil {
				return result
			}
		}
	}

	// 2. Try parameter child (lower priority)
	if node.paramChild != nil {
		params[node.paramChild.paramName] = seg
		if result := t.search(node.paramChild, segments, idx+1, method, params); result != nil {
			return result
		}
		// Backtrack: remove param if this path didn't work
		delete(params, node.paramChild.paramName)
	}

	return nil
}

// searchCompressed handles the case where a radix node's path is a compressed
// prefix and we need to continue matching within the same segment.
func (t *RadixTree) searchCompressed(node *radixNode, remaining string, segments []string, idx int, method string, params map[string]string) *ResolvedRoute {
	if remaining == "" {
		return t.search(node, segments, idx+1, method, params)
	}

	if child := node.findStaticChildForSearch(remaining); child != nil {
		if child.path == remaining {
			return t.search(child, segments, idx+1, method, params)
		} else if strings.HasPrefix(remaining, child.path) {
			return t.searchCompressed(child, remaining[len(child.path):], segments, idx, method, params)
		}
	}

	return nil
}

// findStaticChild finds a child node whose path shares a common prefix with seg.
func (n *radixNode) findStaticChild(seg string) *radixNode {
	if len(seg) == 0 {
		return nil
	}
	return n.children[seg[0]]
}

// findStaticChildForSearch finds a child whose path is a prefix of (or equal to) seg.
func (n *radixNode) findStaticChildForSearch(seg string) *radixNode {
	if len(seg) == 0 {
		return nil
	}
	child, ok := n.children[seg[0]]
	if !ok {
		return nil
	}
	// Check that the child's path actually matches the beginning of seg
	if strings.HasPrefix(seg, child.path) || strings.HasPrefix(child.path, seg) {
		return child
	}
	return nil
}

// splitPath splits a URL path into segments.
// e.g., "/api/users/:id" -> ["api", "users", ":id"]
func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return []string{}
	}
	return strings.Split(path, "/")
}

// longestCommonPrefix returns the length of the longest common prefix
// between two strings.
func longestCommonPrefix(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	for i := 0; i < max; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return max
}
