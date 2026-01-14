package transform

import (
	"math/big"
	"strconv"
	"strings"
	"time"
)

// parseHexInt parses a hex string "0x..." or decimal string to int64.
func parseHexInt(s string) int64 {
	if s == "" {
		return 0
	}
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		n := new(big.Int)
		n.SetString(s[2:], 16)
		return n.Int64()
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// parseHexBigInt parses a hex string to a big.Int (for values that may exceed int64).
func parseHexBigInt(s string) *big.Int {
	if s == "" {
		return big.NewInt(0)
	}
	n := new(big.Int)
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		n.SetString(s[2:], 16)
	} else {
		n.SetString(s, 10)
	}
	return n
}

// formatDecimalOrEmpty formats a hex topic to decimal string, or returns empty if input is empty.
func formatDecimalOrEmpty(hexTopic string) string {
	if hexTopic == "" {
		return ""
	}
	n := parseHexBigInt(hexTopic)
	return n.String()
}

// partitionDate converts a Unix timestamp to YYYY-MM-DD string.
func partitionDate(ts int64) string {
	t := time.Unix(ts, 0).UTC()
	return t.Format("2006-01-02")
}

// hexToInt64Str converts hex string to decimal int64 string for CSV.
func hexToInt64Str(s string) string {
	if s == "" {
		return ""
	}
	return strconv.FormatInt(parseHexInt(s), 10)
}

// hexToBigIntStr converts hex string to big decimal string for large values.
func hexToBigIntStr(s string) string {
	if s == "" {
		return ""
	}
	return parseHexBigInt(s).String()
}

// revertBoolStr converts a Go bool to "true"/"false" string.
func revertBoolStr(hasError bool) string {
	if hasError {
		return "true"
	}
	return "false"
}

// normalizeAddress converts empty address to "0x" for contract creation transactions.
// Golden data represents nil/empty To addresses as "0x" rather than empty string.
func normalizeAddress(addr string) string {
	if addr == "" {
		return "0x"
	}
	return addr
}
