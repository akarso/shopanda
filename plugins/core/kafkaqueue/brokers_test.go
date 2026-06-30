package kafkaqueue_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/plugins/core/kafkaqueue"
)

func TestResolveBrokers_ConfigFirst(t *testing.T) {
	got := kafkaqueue.ResolveBrokers(config.KafkaQueueConfig{
		Brokers: []string{"cfg:9092"},
	})
	if len(got) != 1 || got[0] != "cfg:9092" {
		t.Fatalf("ResolveBrokers() = %v, want [cfg:9092]", got)
	}
}

func TestResolveBrokers_KafkaBrokersEnv(t *testing.T) {
	t.Setenv("SHOPANDA_QUEUE_KAFKA_BROKERS", "")
	t.Setenv("KAFKA_BROKERS", "127.0.0.1:9092,127.0.0.1:9093")

	got := kafkaqueue.ResolveBrokers(config.KafkaQueueConfig{})
	if len(got) != 2 || got[0] != "127.0.0.1:9092" || got[1] != "127.0.0.1:9093" {
		t.Fatalf("ResolveBrokers() = %v, want env brokers", got)
	}
}

func TestResolveBrokers_NamespacedEnvWinsOverKafkaBrokers(t *testing.T) {
	t.Setenv("SHOPANDA_QUEUE_KAFKA_BROKERS", "namespaced:9092")
	t.Setenv("KAFKA_BROKERS", "127.0.0.1:9092")

	got := kafkaqueue.ResolveBrokers(config.KafkaQueueConfig{})
	if len(got) != 1 || got[0] != "namespaced:9092" {
		t.Fatalf("ResolveBrokers() = %v, want namespaced broker", got)
	}
}
