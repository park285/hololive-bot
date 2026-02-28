package sliceutil

// RecursiveNode represents a tree node whose children have the same node type.
// N recursively references this constraint to model self-referential generic trees.
type RecursiveNode[T any, N RecursiveNode[T, N]] interface {
	Value() T
	Children() []N
}

// FlattenTreeValues returns node values in depth-first pre-order.
func FlattenTreeValues[T any, N RecursiveNode[T, N]](roots []N) []T {
	if len(roots) == 0 {
		return []T{}
	}

	values := make([]T, 0, len(roots))
	stack := append([]N(nil), roots...)

	for len(stack) > 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]

		values = append(values, node.Value())
		children := node.Children()

		// Push in reverse to preserve left-to-right order in DFS pre-order.
		for i := len(children) - 1; i >= 0; i-- {
			stack = append(stack, children[i])
		}
	}

	return values
}
