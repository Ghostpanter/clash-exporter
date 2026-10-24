package collector

func policyOf(chains []string) string {
	if len(chains) == 0 || chains[0] == "" {
		return "unknown"
	}
	// Mihomo Chain.Last() is chains[0]: the selected outbound proxy.
	return chains[0]
}

func destinationOf(m Metadata, collect bool) string {
	if !collect {
		return ""
	}
	switch {
	case m.SniffHost != "":
		return m.SniffHost
	case m.Host != "":
		return m.Host
	default:
		return m.DestinationIP
	}
}

func nonNegativeDelta(cur, prev int64) float64 {
	if cur < prev {
		return 0
	}
	return float64(cur - prev)
}
