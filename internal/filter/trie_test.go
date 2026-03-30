package filter

import "testing"

func TestTrie_HasPrefix(t *testing.T) {
	trie := NewTrie([]string{`Symfony\`, `Doctrine\`, `App\`})

	tests := []struct {
		input string
		want  bool
	}{
		{`Symfony\Component\HttpFoundation\Request`, true},
		{`Doctrine\ORM\EntityManager`, true},
		{`App\Service\FooService`, true},
		{`App\`, true},
		{`Ap`, false},    // partial prefix, no match
		{`Symfon`, false}, // partial prefix
		{`strlen`, false},
		{``, false},
		{`Symfony\`, true}, // exact prefix match
	}

	for _, tt := range tests {
		got := trie.HasPrefix(tt.input)
		if got != tt.want {
			t.Errorf("HasPrefix(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestTrie_Empty(t *testing.T) {
	trie := NewTrie(nil)
	if trie.HasPrefix("anything") {
		t.Error("empty trie should match nothing")
	}
}

func TestTrie_Insert(t *testing.T) {
	trie := NewTrie([]string{`Foo\`})
	if trie.HasPrefix(`Foo\Bar`) != true {
		t.Error("should match after initial insert")
	}
	trie.Insert(`Bar\`)
	if trie.HasPrefix(`Bar\Baz`) != true {
		t.Error("should match after dynamic insert")
	}
}
