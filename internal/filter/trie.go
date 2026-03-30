package filter

// Trie is a prefix tree for fast namespace prefix matching.
type Trie struct {
	children [256]*Trie
	terminal bool // true if this node marks the end of a prefix
}

// NewTrie builds a trie from a list of prefixes.
func NewTrie(prefixes []string) *Trie {
	t := &Trie{}
	for _, p := range prefixes {
		t.Insert(p)
	}
	return t
}

// Insert adds a prefix to the trie.
func (t *Trie) Insert(prefix string) {
	node := t
	for i := 0; i < len(prefix); i++ {
		c := prefix[i]
		if node.children[c] == nil {
			node.children[c] = &Trie{}
		}
		node = node.children[c]
	}
	node.terminal = true
}

// HasPrefix returns true if s starts with any prefix stored in the trie.
func (t *Trie) HasPrefix(s string) bool {
	node := t
	for i := 0; i < len(s); i++ {
		if node.terminal {
			return true
		}
		c := s[i]
		if node.children[c] == nil {
			return false
		}
		node = node.children[c]
	}
	return node.terminal
}
