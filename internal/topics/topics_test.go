package topics

import (
	"testing"

	"github.com/mdarkanurl/JanoLuck/internal/kafka"
)

func TestAllTopicsAreUniqueAndNamed(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("All() returned no topics")
	}

	seen := make(map[string]struct{}, len(all))
	for _, topic := range all {
		if topic.Name == "" {
			t.Error("topic without a name")
			continue
		}
		if _, exists := seen[topic.Name]; exists {
			t.Errorf("duplicate topic %q", topic.Name)
		}
		seen[topic.Name] = struct{}{}
	}
}

func TestAllTopicsMatchKafkaTopics(t *testing.T) {
	var _ []kafka.Topic = All()
}
