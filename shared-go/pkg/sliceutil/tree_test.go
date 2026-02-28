package sliceutil

import "testing"

type testTreeNode struct {
	v        string
	children []*testTreeNode
}

func (n *testTreeNode) Value() string {
	return n.v
}

func (n *testTreeNode) Children() []*testTreeNode {
	return n.children
}

func TestFlattenTreeValues(t *testing.T) {
	root := &testTreeNode{
		v: "root",
		children: []*testTreeNode{
			{
				v: "left",
				children: []*testTreeNode{
					{v: "left.left"},
				},
			},
			{v: "right"},
		},
	}

	got := FlattenTreeValues[string]([]*testTreeNode{root})
	want := []string{"root", "left", "left.left", "right"}

	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}

	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFlattenTreeValues_Empty(t *testing.T) {
	got := FlattenTreeValues[string]([]*testTreeNode{})
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}
