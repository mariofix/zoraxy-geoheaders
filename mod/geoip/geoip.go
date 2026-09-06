package geoip

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
)

type ipRangeV4 struct {
	startIP uint32
	endIP   uint32
	ccIdx   uint16
}

type ipRangeV6 struct {
	startHigh uint64
	startLow  uint64
	endHigh   uint64
	endLow    uint64
	ccIdx     uint16
}

type GeoDB struct {
	mu           sync.RWMutex
	rangesV4     []ipRangeV4
	rangesV6     []ipRangeV6
	ccTable      []string
	ccToIndex    map[string]uint16
	customRanges map[string]string // CIDR or IP -> CC
	customNets   []customCIDRMatch
	fallbackCC   string
	localCC      string
}

type customCIDRMatch struct {
	ipnet       *net.IPNet
	countryCode string
}

type GeoDBOptions struct {
	FallbackCountryCode string
	LocalCountryCode    string
	IPv4CSVPath         string
	IPv6CSVPath         string
}

func NewGeoDB(opts *GeoDBOptions) *GeoDB {
	if opts == nil {
		opts = &GeoDBOptions{}
	}
	fallback := opts.FallbackCountryCode
	if fallback == "" {
		fallback = "XX"
	}
	local := opts.LocalCountryCode
	if local == "" {
		local = "LOCAL"
	}

	g := &GeoDB{
		ccTable:      make([]string, 0),
		ccToIndex:    make(map[string]uint16),
		customRanges: make(map[string]string),
		customNets:   make([]customCIDRMatch, 0),
		fallbackCC:   fallback,
		localCC:      local,
	}

	// Populate base built-in ranges for popular ranges & tests
	g.loadBuiltinDataset()

	// Load external CSVs if available
	if opts.IPv4CSVPath != "" {
		_ = g.LoadIPv4CSVFile(opts.IPv4CSVPath)
	}
	if opts.IPv6CSVPath != "" {
		_ = g.LoadIPv6CSVFile(opts.IPv6CSVPath)
	}

	return g
}

func (g *GeoDB) getOrCreateCCIndex(cc string) uint16 {
	cc = strings.ToUpper(strings.TrimSpace(cc))
	if idx, exists := g.ccToIndex[cc]; exists {
		return idx
	}
	idx := uint16(len(g.ccTable))
	g.ccTable = append(g.ccTable, cc)
	g.ccToIndex[cc] = idx
	return idx
}

func (g *GeoDB) SetFallbackCountryCode(cc string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if cc != "" {
		g.fallbackCC = strings.ToUpper(strings.TrimSpace(cc))
	}
}

func (g *GeoDB) GetFallbackCountryCode() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.fallbackCC
}

func (g *GeoDB) SetLocalCountryCode(cc string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if cc != "" {
		g.localCC = strings.ToUpper(strings.TrimSpace(cc))
	}
}

func (g *GeoDB) GetLocalCountryCode() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.localCC
}

func (g *GeoDB) RangeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.rangesV4) + len(g.rangesV6) + len(g.customRanges)
}

func (g *GeoDB) AddCustomOverride(cidrOrIP string, countryCode string) error {
	return g.SetCustomOverride(cidrOrIP, countryCode)
}

func (g *GeoDB) SetCustomOverride(cidrOrIP string, countryCode string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	cidrOrIP = strings.TrimSpace(cidrOrIP)
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if cidrOrIP == "" || countryCode == "" {
		return fmt.Errorf("invalid override rule")
	}

	if !strings.Contains(cidrOrIP, "/") {
		parsedIP := net.ParseIP(cidrOrIP)
		if parsedIP == nil {
			return fmt.Errorf("invalid IP address: %s", cidrOrIP)
		}
		if parsedIP.To4() != nil {
			cidrOrIP = cidrOrIP + "/32"
		} else {
			cidrOrIP = cidrOrIP + "/128"
		}
	}

	_, _, err := net.ParseCIDR(cidrOrIP)
	if err != nil {
		return fmt.Errorf("invalid CIDR: %w", err)
	}

	g.customRanges[cidrOrIP] = countryCode

	// Rebuild customNets list
	g.rebuildCustomNets()
	return nil
}

func (g *GeoDB) DeleteCustomOverride(cidrOrIP string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.customRanges, cidrOrIP)
	if !strings.Contains(cidrOrIP, "/") {
		delete(g.customRanges, cidrOrIP+"/32")
		delete(g.customRanges, cidrOrIP+"/128")
	}
	g.rebuildCustomNets()
}

func (g *GeoDB) GetCustomOverrides() map[string]string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make(map[string]string, len(g.customRanges))
	for k, v := range g.customRanges {
		out[k] = v
	}
	return out
}

func (g *GeoDB) rebuildCustomNets() {
	list := make([]customCIDRMatch, 0, len(g.customRanges))
	for cidr, cc := range g.customRanges {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil {
			list = append(list, customCIDRMatch{
				ipnet:       ipnet,
				countryCode: cc,
			})
		}
	}
	// Sort by prefix length descending (most specific first)
	sort.Slice(list, func(i, j int) bool {
		sizeI, _ := list[i].ipnet.Mask.Size()
		sizeJ, _ := list[j].ipnet.Mask.Size()
		return sizeI > sizeJ
	})
	g.customNets = list
}

func (g *GeoDB) LoadIPv4CSVBytes(content []byte) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	r := csv.NewReader(bytes.NewReader(content))
	var newRanges []ipRangeV4

	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if len(rec) < 3 {
			continue
		}
		startIP := net.ParseIP(strings.TrimSpace(rec[0]))
		endIP := net.ParseIP(strings.TrimSpace(rec[1]))
		cc := strings.ToUpper(strings.TrimSpace(rec[2]))

		if startIP == nil || endIP == nil || cc == "" {
			continue
		}
		s4 := startIP.To4()
		e4 := endIP.To4()
		if s4 == nil || e4 == nil {
			continue
		}

		sVal := binary.BigEndian.Uint32(s4)
		eVal := binary.BigEndian.Uint32(e4)
		if sVal > eVal {
			sVal, eVal = eVal, sVal
		}

		ccIdx := g.getOrCreateCCIndex(cc)
		newRanges = append(newRanges, ipRangeV4{
			startIP: sVal,
			endIP:   eVal,
			ccIdx:   ccIdx,
		})
	}

	sort.Slice(newRanges, func(i, j int) bool {
		return newRanges[i].startIP < newRanges[j].startIP
	})

	g.rangesV4 = newRanges
	return nil
}

func (g *GeoDB) LoadIPv4CSVFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	return g.LoadIPv4CSVBytes(data)
}

func (g *GeoDB) LoadIPv6CSVBytes(content []byte) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	r := csv.NewReader(bytes.NewReader(content))
	var newRanges []ipRangeV6

	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if len(rec) < 3 {
			continue
		}
		startIP := net.ParseIP(strings.TrimSpace(rec[0]))
		endIP := net.ParseIP(strings.TrimSpace(rec[1]))
		cc := strings.ToUpper(strings.TrimSpace(rec[2]))

		if startIP == nil || endIP == nil || cc == "" {
			continue
		}
		s16 := startIP.To16()
		e16 := endIP.To16()
		if s16 == nil || e16 == nil {
			continue
		}

		sHigh := binary.BigEndian.Uint64(s16[0:8])
		sLow := binary.BigEndian.Uint64(s16[8:16])
		eHigh := binary.BigEndian.Uint64(e16[0:8])
		eLow := binary.BigEndian.Uint64(e16[8:16])

		ccIdx := g.getOrCreateCCIndex(cc)
		newRanges = append(newRanges, ipRangeV6{
			startHigh: sHigh,
			startLow:  sLow,
			endHigh:   eHigh,
			endLow:    eLow,
			ccIdx:     ccIdx,
		})
	}

	sort.Slice(newRanges, func(i, j int) bool {
		if newRanges[i].startHigh != newRanges[j].startHigh {
			return newRanges[i].startHigh < newRanges[j].startHigh
		}
		return newRanges[i].startLow < newRanges[j].startLow
	})

	g.rangesV6 = newRanges
	return nil
}

func (g *GeoDB) LoadIPv6CSVFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	return g.LoadIPv6CSVBytes(data)
}

func (g *GeoDB) ResolveIP(ipStr string) string {
	ipStr = strings.TrimSpace(ipStr)
	if strings.Contains(ipStr, ",") {
		// Proxied list: take first IP
		ipStr = strings.TrimSpace(strings.Split(ipStr, ",")[0])
	}
	if host, _, err := net.SplitHostPort(ipStr); err == nil {
		ipStr = host
	}

	parsedIP := net.ParseIP(ipStr)
	if parsedIP == nil {
		g.mu.RLock()
		defer g.mu.RUnlock()
		return g.fallbackCC
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	// 1. Check custom CIDR overrides first
	for _, custom := range g.customNets {
		if custom.ipnet.Contains(parsedIP) {
			return custom.countryCode
		}
	}

	// 2. Check special / reserved ranges (loopback, private, carrier nat, link local)
	if isPrivateOrSpecialIP(parsedIP) {
		return g.localCC
	}

	// 3. Check IPv4 ranges
	if ipv4 := parsedIP.To4(); ipv4 != nil {
		val := binary.BigEndian.Uint32(ipv4)
		cc := g.searchIPv4(val)
		if cc != "" {
			return cc
		}
		return g.fallbackCC
	}

	// 4. Check IPv6 ranges
	if ipv6 := parsedIP.To16(); ipv6 != nil {
		high := binary.BigEndian.Uint64(ipv6[0:8])
		low := binary.BigEndian.Uint64(ipv6[8:16])
		cc := g.searchIPv6(high, low)
		if cc != "" {
			return cc
		}
		return g.fallbackCC
	}

	return g.fallbackCC
}

func (g *GeoDB) searchIPv4(val uint32) string {
	if len(g.rangesV4) == 0 {
		return ""
	}

	idx := sort.Search(len(g.rangesV4), func(i int) bool {
		return g.rangesV4[i].startIP > val
	})

	if idx == 0 {
		return ""
	}

	candidate := g.rangesV4[idx-1]
	if val >= candidate.startIP && val <= candidate.endIP {
		return g.ccTable[candidate.ccIdx]
	}
	return ""
}

func (g *GeoDB) searchIPv6(high, low uint64) string {
	if len(g.rangesV6) == 0 {
		return ""
	}

	idx := sort.Search(len(g.rangesV6), func(i int) bool {
		if g.rangesV6[i].startHigh != high {
			return g.rangesV6[i].startHigh > high
		}
		return g.rangesV6[i].startLow > low
	})

	if idx == 0 {
		return ""
	}

	candidate := g.rangesV6[idx-1]
	if candidate.startHigh <= high && high <= candidate.endHigh {
		if (candidate.startHigh < high || candidate.startLow <= low) &&
			(high < candidate.endHigh || low <= candidate.endLow) {
			return g.ccTable[candidate.ccIdx]
		}
	}
	return ""
}

func IsPrivateOrSpecialIP(ipStr string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return false
	}
	return isPrivateOrSpecialIP(ip)
}

func isPrivateOrSpecialIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// Carrier-grade NAT (100.64.0.0/10)
	if ipv4 := ip.To4(); ipv4 != nil {
		if ipv4[0] == 100 && (ipv4[1]&0xC0) == 64 {
			return true
		}
	}
	return false
}

func ExtractClientIP(r *http.Request) string {
	// 1. CF-Connecting-IP
	if cfIP := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cfIP != "" {
		return cfIP
	}

	// 2. X-Forwarded-For (client IP is first)
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}

	// 3. X-Real-IP
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}

	// 4. RemoteAddr
	if r.RemoteAddr != "" {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			return host
		}
		return r.RemoteAddr
	}

	return ""
}
