package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// SplitList splits a comma-separated list, trimming and dropping blanks.
func SplitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// JoinList is the inverse of SplitList.
func JoinList(l []string) string { return strings.Join(l, ", ") }

// ParseLimit parses a count: -1 (or "unlimited", blank) means unlimited.
func ParseLimit(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "", "-1", "unlimited", "none":
		return -1, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("a whole number, or -1 for unlimited")
	}
	return n, nil
}

// ParseBytes parses a size: a number with an optional k/m/g/t suffix (decimal)
// or kib/mib/gib/tib (binary); -1 or blank is unlimited.
func ParseBytes(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "", "-1", "unlimited", "none":
		return -1, nil
	}
	mult := int64(1)
	units := []struct {
		suffix string
		mult   int64
	}{
		{"kib", 1 << 10}, {"mib", 1 << 20}, {"gib", 1 << 30}, {"tib", 1 << 40},
		{"kb", 1000}, {"mb", 1000_000}, {"gb", 1000_000_000}, {"tb", 1000_000_000_000},
		{"k", 1000}, {"m", 1000_000}, {"g", 1000_000_000}, {"t", 1000_000_000_000},
		{"b", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			mult = u.mult
			s = strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			break
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("a size like 1000, 10k, 5m, 2g (or kib/mib/gib), or -1 for unlimited")
	}
	return int64(math.Round(f * float64(mult))), nil
}

// Bytes formats a size: "unlimited" for -1, human units otherwise.
func Bytes(n int64) string {
	if n < 0 {
		return "unlimited"
	}
	return Size(uint64(n))
}

// Size formats a byte count with binary units.
func Size(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Limit formats a count: "unlimited" for -1.
func Limit(n int64) string {
	if n < 0 {
		return "unlimited"
	}
	return strconv.FormatInt(n, 10)
}

// Count formats a count with thousands separators.
func Count(n uint64) string {
	s := strconv.FormatUint(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// DurationText renders a duration the way nats takes it back: "1h30m",
// "2m", "500ms"; zero is "" (unlimited / not set) and negative is "-1".
func DurationText(d time.Duration) string {
	switch {
	case d == 0:
		return ""
	case d < 0:
		return "-1"
	}
	if d%(24*time.Hour) == 0 && d >= 24*time.Hour {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	return compactDuration(d)
}

// compactDuration is Go's duration text without the zero units that
// String() keeps: "1h30m" rather than "1h30m0s".
func compactDuration(d time.Duration) string {
	if d < time.Second {
		return d.String()
	}
	if d%time.Second != 0 {
		return d.String()
	}
	h, m, s := d/time.Hour, (d%time.Hour)/time.Minute, (d%time.Minute)/time.Second
	var b strings.Builder
	if h > 0 {
		fmt.Fprintf(&b, "%dh", h)
	}
	if m > 0 {
		fmt.Fprintf(&b, "%dm", m)
	}
	if s > 0 {
		fmt.Fprintf(&b, "%ds", s)
	}
	return b.String()
}

// ParseDuration parses what nats accepts: Go durations plus d/w/M/y
// units; "" and "-1" are unlimited (0 and -1). It only validates here —
// the text is passed to nats as typed.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "", "0":
		return 0, nil
	case "-1", "unlimited", "never":
		return -1, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	// d / w / M / y suffixes, possibly with a Go tail: 1d12h
	total := time.Duration(0)
	rest := s
	for rest != "" {
		i := 0
		for i < len(rest) && (rest[i] >= '0' && rest[i] <= '9' || rest[i] == '.') {
			i++
		}
		if i == 0 || i == len(rest) {
			return 0, fmt.Errorf("a duration like 30s, 5m, 2h, 1d, 1w, 1y")
		}
		n, err := strconv.ParseFloat(rest[:i], 64)
		if err != nil {
			return 0, fmt.Errorf("a duration like 30s, 5m, 2h, 1d, 1w, 1y")
		}
		var unit time.Duration
		switch rest[i] {
		case 'd':
			unit = 24 * time.Hour
		case 'w':
			unit = 7 * 24 * time.Hour
		case 'M':
			unit = 30 * 24 * time.Hour
		case 'y':
			unit = 365 * 24 * time.Hour
		default:
			d, err := time.ParseDuration(rest)
			if err != nil {
				return 0, fmt.Errorf("a duration like 30s, 5m, 2h, 1d, 1w, 1y")
			}
			return total + d, nil
		}
		total += time.Duration(n * float64(unit))
		rest = rest[i+1:]
	}
	return total, nil
}

// ValidDuration is a field validator for durations ("" allowed).
func ValidDuration(s string) error {
	_, err := ParseDuration(s)
	return err
}

// HumanDuration renders a duration compactly: "1h30m", "2d", "unlimited".
func HumanDuration(d time.Duration) string {
	switch {
	case d == 0:
		return "unlimited"
	case d < 0:
		return "unlimited"
	case d < time.Second:
		return d.String()
	}
	d = d.Round(time.Second)
	if d >= 48*time.Hour && d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	return compactDuration(d)
}

// Ago renders how long ago t was: "12s", "3m", "2h", "5d"; "never" for a
// zero time.
func Ago(t time.Time, now time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return "never"
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		return "now"
	case d < time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// Date renders a time as yyyy-mm-dd hh:mm:ss in local time.
func Date(t time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// ShortID shortens a server or key id: first 8 characters and "…".
func ShortID(s string) string {
	r := []rune(s)
	if len(r) <= 12 {
		return s
	}
	return string(r[:8]) + "…"
}

// ServerLabel is a server name for a header: the name as is, or a shortened
// id when the server has none (the id is then its name).
func ServerLabel(name string) string {
	if len(name) == 56 && strings.HasPrefix(name, "N") && strings.ToUpper(name) == name {
		return ShortID(name)
	}
	return name
}

// Metadata renders a metadata map as sorted "k=v" items, hiding the
// _nats.* entries the server adds.
func Metadata(m map[string]string) []string {
	var out []string
	for _, k := range sortedKeys(m) {
		if strings.HasPrefix(k, "_nats.") {
			continue
		}
		out = append(out, k+"="+m[k])
	}
	return out
}

// ParseMetadata reads "k=v" items into a map.
func ParseMetadata(items []string) (map[string]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, it := range items {
		k, v, ok := strings.Cut(it, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("metadata is written key=value")
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func sortStrings(l []string) {
	for i := 1; i < len(l); i++ {
		for j := i; j > 0 && l[j] < l[j-1]; j-- {
			l[j], l[j-1] = l[j-1], l[j]
		}
	}
}
