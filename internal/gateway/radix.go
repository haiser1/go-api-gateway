package gateway

import (
	"fmt"
	"strings"
)

// radixNode represents a node in the radix tree.
// Each node can hold handlers keyed by HTTP method,
// and may have static children, a parameter child (:param), or a wildcard child (*).
type radixNode struct {
	// path is this node's segment of the route (e.g. "api", "users")
	path string

	// children holds static child nodes keyed by first character of their path.
	// Lookup is O(1) via map — fast enough for typical fan-out (<20 children).
	children map[byte]*radixNode

	// paramChild is a single parameter child (e.g. :id).
	// Only one param child is allowed per node to avoid ambiguity.
	paramChild *radixNode
	paramName  string

	// wildcardChild is a catch-all child (e.g. *filepath).
	// It matches all remaining segments and must be the last segment in a route.
	wildcardChild *radixNode
	wildcardName  string

	// handlers maps HTTP method -> ResolvedRoute for this node.
	// nil if this node is not a terminal node (i.e. no route ends here).
	handlers map[string]*ResolvedRoute
}

// RadixTree is a segment-level compressed radix tree for fast HTTP route lookup.
// Routes are split by "/" into segments, and common prefixes within segments
// are compressed into shared nodes to minimize memory usage and tree depth.
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
// Returns ErrDuplicateRoute if a handler for the same method+path already exists.
//
// Path format examples:
//   - "/api/users"                → static route
//   - "/api/users/:id"            → parameterized route
//   - "/api/users/:id/posts"      → nested param
//   - "/static/*filepath"         → wildcard catch-all (matches all remaining segments)
//
// Wildcard segments must be the last segment in the path.
func (t *RadixTree) Insert(method, path string, route *ResolvedRoute) error {
	current := t.root

	forEachSegment(path, func(seg string) bool {
		if strings.HasPrefix(seg, "*") {
			// Wildcard catch-all segment — must be the last segment
			wildcardName := seg[1:]
			if wildcardName == "" {
				wildcardName = "wildcard"
			}
			if current.wildcardChild == nil {
				current.wildcardChild = &radixNode{
					path:         seg,
					children:     make(map[byte]*radixNode),
					wildcardName: wildcardName,
				}
			}
			current = current.wildcardChild
			return false // wildcard must be last segment, stop iteration
		}

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
			return true
		}

		// Static segment — try to find a matching child
		child := current.findStaticChild(seg)
		if child != nil {
			// Check if we need to split the existing node
			commonLen := longestCommonPrefix(child.path, seg)

			if commonLen < len(child.path) {
				// Split the existing child: create a new node for the shared prefix,
				// and push the old child down as a child of the prefix node.
				splitChild := &radixNode{
					path:          child.path[commonLen:],
					children:      child.children,
					paramChild:    child.paramChild,
					paramName:     child.paramName,
					wildcardChild: child.wildcardChild,
					wildcardName:  child.wildcardName,
					handlers:      child.handlers,
				}

				child.path = child.path[:commonLen]
				child.children = make(map[byte]*radixNode)
				child.children[splitChild.path[0]] = splitChild
				child.paramChild = nil
				child.paramName = ""
				child.wildcardChild = nil
				child.wildcardName = ""
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

		return true
	})

	// Reject duplicate: error if a handler already exists for this method+path
	if current.handlers != nil {
		if _, exists := current.handlers[method]; exists {
			return fmt.Errorf("duplicate route: %s %s", method, path)
		}
	}

	// Set handler on the final node
	if current.handlers == nil {
		current.handlers = make(map[string]*ResolvedRoute)
	}
	current.handlers[method] = route
	return nil
}

// Search looks up a route by method and path.
// Returns the matched route and extracted path parameters (nil if no params).
//
// Priority order: static children > param child > wildcard child.
// The params map is lazily allocated — zero allocations for static-only routes.
func (t *RadixTree) Search(method, path string) (*ResolvedRoute, map[string]string) {
	var params map[string]string
	route := t.searchIterative(method, path, &params)
	if route != nil {
		return route, params
	}
	return nil, nil
}

// searchIterative walks the tree segment by segment without allocating a []string.
// It uses an iterative approach for static/param segments and only recurses
// for backtracking when a static match fails and a param fallback is needed.
func (t *RadixTree) searchIterative(method, path string, params *map[string]string) *ResolvedRoute {
	path = strings.TrimLeft(path, "/")
	return t.searchNode(t.root, method, path, params)
}

// searchNode performs the recursive search from a given node.
// remaining is the un-consumed portion of the URL path (without leading slash).
func (t *RadixTree) searchNode(node *radixNode, method, remaining string, params *map[string]string) *ResolvedRoute {
	// Base case: all segments consumed
	if remaining == "" {
		if node.handlers != nil {
			if route, ok := node.handlers[method]; ok {
				return route
			}
		}
		return nil
	}

	// Extract the next segment from remaining
	seg, rest := nextSegment(remaining)

	// 1. Try static children first (highest priority)
	if child := node.findStaticChildForSearch(seg); child != nil {
		if child.path == seg {
			// Exact match on this segment
			if result := t.searchNode(child, method, rest, params); result != nil {
				return result
			}
		} else if strings.HasPrefix(seg, child.path) {
			// The child's path is a prefix of our segment — continue matching
			innerRemaining := seg[len(child.path):]
			if result := t.searchCompressedNode(child, innerRemaining, method, rest, params); result != nil {
				return result
			}
		}
	}

	// 2. Try parameter child (lower priority)
	if node.paramChild != nil {
		// Lazy allocation: only create params map when first param is found
		if *params == nil {
			*params = make(map[string]string)
		}
		(*params)[node.paramChild.paramName] = seg
		if result := t.searchNode(node.paramChild, method, rest, params); result != nil {
			return result
		}
		// Backtrack: remove param if this path didn't work
		delete(*params, node.paramChild.paramName)
	}

	// 3. Try wildcard catch-all (lowest priority — matches all remaining)
	if node.wildcardChild != nil {
		if *params == nil {
			*params = make(map[string]string)
		}
		// Wildcard captures everything from this segment onwards
		(*params)[node.wildcardChild.wildcardName] = remaining
		if node.wildcardChild.handlers != nil {
			if route, ok := node.wildcardChild.handlers[method]; ok {
				return route
			}
		}
		// Backtrack
		delete(*params, node.wildcardChild.wildcardName)
	}

	return nil
}

// searchCompressedNode handles the case where a radix node's path is a compressed
// prefix and we need to continue matching within the same segment.
func (t *RadixTree) searchCompressedNode(node *radixNode, innerRemaining, method, rest string, params *map[string]string) *ResolvedRoute {
	if innerRemaining == "" {
		return t.searchNode(node, method, rest, params)
	}

	if child := node.findStaticChildForSearch(innerRemaining); child != nil {
		if child.path == innerRemaining {
			return t.searchNode(child, method, rest, params)
		} else if strings.HasPrefix(innerRemaining, child.path) {
			return t.searchCompressedNode(child, innerRemaining[len(child.path):], method, rest, params)
		}
	}

	return nil
}

// findStaticChild finds a child node whose path shares a common prefix with seg.
// Used during Insert to locate the correct insertion point.
func (n *radixNode) findStaticChild(seg string) *radixNode {
	if len(seg) == 0 {
		return nil
	}
	return n.children[seg[0]]
}

// findStaticChildForSearch finds a child whose path is a prefix of (or equal to) seg.
// Used during Search to match against compressed node paths.
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

// nextSegment extracts the next path segment and returns it along with the rest.
// Example: "api/users/123" → ("api", "users/123")
// Example: "health" → ("health", "")
func nextSegment(path string) (seg, rest string) {
	i := strings.IndexByte(path, '/')
	if i < 0 {
		return path, ""
	}
	return path[:i], path[i+1:]
}

// forEachSegment iterates over each segment in a URL path without allocating a slice.
// The callback returns false to stop iteration (used by wildcard which must be last).
// Example: "/api/users/:id" iterates: "api", "users", ":id"
func forEachSegment(path string, fn func(seg string) bool) {
	path = strings.TrimLeft(path, "/")
	for path != "" {
		i := strings.IndexByte(path, '/')
		if i < 0 {
			fn(path)
			return
		}
		if !fn(path[:i]) {
			return
		}
		path = path[i+1:]
	}
}

// longestCommonPrefix returns the length of the longest common prefix
// between two strings. Used during Insert to determine where to split nodes.
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
