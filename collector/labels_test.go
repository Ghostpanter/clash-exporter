package collector

import "testing"

func TestPolicyOf(t *testing.T) {
	if got := policyOf(nil); got != "unknown" {
		t.Fatalf("empty chains: %q", got)
	}
	if got := policyOf([]string{""}); got != "unknown" {
		t.Fatalf("blank chain: %q", got)
	}
	if got := policyOf([]string{"香港 01", "PROXY"}); got != "香港 01" {
		t.Fatalf("policy: %q", got)
	}
}

func TestDestinationOf(t *testing.T) {
	m := Metadata{Host: "1.2.3.4", SniffHost: "example.com", DestinationIP: "1.2.3.4"}
	if got := destinationOf(m, false); got != "" {
		t.Fatalf("disabled: %q", got)
	}
	if got := destinationOf(m, true); got != "example.com" {
		t.Fatalf("sniff: %q", got)
	}
	m.SniffHost = ""
	if got := destinationOf(m, true); got != "1.2.3.4" {
		t.Fatalf("host: %q", got)
	}
	m.Host = ""
	if got := destinationOf(m, true); got != "1.2.3.4" {
		t.Fatalf("ip: %q", got)
	}
}

func TestNonNegativeDelta(t *testing.T) {
	if got := nonNegativeDelta(10, 4); got != 6 {
		t.Fatalf("delta: %v", got)
	}
	if got := nonNegativeDelta(3, 9); got != 0 {
		t.Fatalf("reset: %v", got)
	}
}
